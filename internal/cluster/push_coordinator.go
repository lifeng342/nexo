package cluster

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mbeoliero/kit/log"

	"github.com/mbeoliero/nexo/internal/entity"
)

var ErrRemoteEnvelopeQueueFull = errors.New("remote envelope queue full")

// DispatchDrainer waits for locally accepted async dispatch work to finish.
// It intentionally excludes inbound remote envelopes so graceful shutdown only
// blocks on backlog this instance accepted itself.
type DispatchDrainer interface {
	WaitDispatchDrained(ctx context.Context) error
}

// LocalDispatcher abstracts the local websocket fanout runtime.
type LocalDispatcher interface {
	DeliverLocal(ctx context.Context, refs []ConnRef, payload *PushPayload) error
	LocalConnRefs(userIds []string, excludeConnId string) map[string][]ConnRef
}

// RemoteRouteReader resolves remote connection refs from the shared route index.
type RemoteRouteReader interface {
	GetUsersConnRefs(ctx context.Context, userIds []string) (map[string][]RouteConn, error)
}

// PushCoordinator owns the route-only realtime fanout path.
// Local messages accepted on this instance and already-routed remote envelopes
// are processed on separate queues so shutdown accounting and overload handling
// can treat them differently.
type PushCoordinator struct {
	currentInstanceId string
	local             LocalDispatcher
	routes            RemoteRouteReader
	bus               PushBus
	now               func() time.Time
	newPushId         func() string
	dispatchCtx       context.Context
	cancelDispatch    context.CancelFunc

	mu      sync.Mutex
	cond    *sync.Cond
	pending int64

	queueSize          int
	workerNum          int
	maxRefsPerEnvelope int
	stats              *RuntimeStats
	startOnce          sync.Once
	dispatchQueue      chan dispatchTask
	remoteQueue        chan *PushEnvelope
	afterTaskQueued    func()
	onRemoteOverflow   func()
	onRouteReadFailure func(error)
	remoteOverflowOnce sync.Once
}

type dispatchTask struct {
	ctx           context.Context
	msg           *entity.Message
	userIds       []string
	excludeConnId string
	env           *PushEnvelope
}

// NewPushCoordinator creates a coordinator with conservative defaults so the
// caller can wire routing, runtime stats, and dispatch sizing incrementally.
func NewPushCoordinator(currentInstanceID string, local LocalDispatcher, routes RemoteRouteReader, bus PushBus) *PushCoordinator {
	dispatchCtx, cancelDispatch := context.WithCancel(context.Background())
	coordinator := &PushCoordinator{
		currentInstanceId:  currentInstanceID,
		local:              local,
		routes:             routes,
		bus:                bus,
		now:                time.Now,
		newPushId:          uuid.NewString,
		dispatchCtx:        dispatchCtx,
		cancelDispatch:     cancelDispatch,
		queueSize:          1024,
		workerNum:          1,
		maxRefsPerEnvelope: 500,
	}
	coordinator.cond = sync.NewCond(&coordinator.mu)
	return coordinator
}

// ConfigureDispatch updates the shared worker and queue sizing used by both the
// local async dispatch path and the inbound remote envelope path.
func (c *PushCoordinator) ConfigureDispatch(queueSize, workerNum, maxRefsPerEnvelope int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if queueSize > 0 {
		c.queueSize = queueSize
	}
	if workerNum > 0 {
		c.workerNum = workerNum
	}
	if maxRefsPerEnvelope > 0 {
		c.maxRefsPerEnvelope = maxRefsPerEnvelope
	}
}

// SetRuntimeStats installs the in-memory stats sink used by readiness and
// operator-facing diagnostics.
func (c *PushCoordinator) SetRuntimeStats(stats *RuntimeStats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats = stats
}

// SetRemoteOverflowHandler registers the one-shot callback invoked when the
// inbound remote queue cannot accept more work and the instance should degrade.
func (c *PushCoordinator) SetRemoteOverflowHandler(handler func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onRemoteOverflow = handler
}

// SetRouteReadFailureHandler registers a callback for route-store read failures.
func (c *PushCoordinator) SetRouteReadFailureHandler(handler func(error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onRouteReadFailure = handler
}

// AsyncPushToUsers schedules best-effort realtime fanout for a persisted
// message. Queue saturation degrades realtime delivery but does not fail the
// original write path.
func (c *PushCoordinator) AsyncPushToUsers(msg *entity.Message, userIds []string, excludeConnId string) {
	if msg == nil || len(userIds) == 0 || c.local == nil {
		return
	}

	c.enqueueTask(dispatchTask{
		ctx:           c.currentDispatchContext(),
		msg:           msg,
		userIds:       append([]string(nil), userIds...),
		excludeConnId: excludeConnId,
	})
}

// EnqueueRemoteEnvelope accepts already-routed cross-instance pushes for local
// delivery. Overflow is treated as a node health fault so callers can stop
// routing traffic here instead of silently dropping remote delivery.
func (c *PushCoordinator) EnqueueRemoteEnvelope(ctx context.Context, env *PushEnvelope) {
	if env == nil || env.Payload == nil || c.local == nil {
		return
	}
	c.ensureWorkers()
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case c.remoteQueue <- env:
	case <-ctx.Done():
	case <-c.currentDispatchContext().Done():
	default:
		log.CtxError(ctx, "remote envelope queue full: source_instance=%s", c.currentInstanceId)
		c.triggerRemoteOverflow()
	}
}

// WaitDispatchDrained waits for locally accepted async dispatch work to finish.
// Inbound remote delivery is intentionally excluded from this counter because
// graceful shutdown only needs to flush work accepted by this instance.
func (c *PushCoordinator) WaitDispatchDrained(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		c.mu.Lock()
		pending := c.pending
		c.mu.Unlock()
		if pending == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *PushCoordinator) PendingDispatch() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending
}

// CancelDispatches stops local async dispatch workers so a forced shutdown does
// not let accepted realtime fanout outlive the shared drain deadline.
func (c *PushCoordinator) CancelDispatches() {
	c.mu.Lock()
	cancelDispatch := c.cancelDispatch
	c.mu.Unlock()
	if cancelDispatch != nil {
		cancelDispatch()
	}
}

// HandleRemoteEnvelope performs local-only delivery for the current instance's
// refs without re-reading routes from Redis.
func (c *PushCoordinator) HandleRemoteEnvelope(ctx context.Context, env *PushEnvelope) {
	if env == nil || env.Payload == nil || c.local == nil {
		return
	}
	refs := env.TargetConnMap[c.currentInstanceId]
	if len(refs) == 0 {
		return
	}
	_ = c.local.DeliverLocal(ctx, refs, env.Payload)
}

func (c *PushCoordinator) enqueueTask(task dispatchTask) {
	c.ensureWorkers()
	if task.ctx == nil {
		task.ctx = context.Background()
	}

	c.addPending()
	select {
	case c.dispatchQueue <- task:
		if c.afterTaskQueued != nil {
			c.afterTaskQueued()
		}
		c.updateDispatchQueueDepth()
	default:
		c.donePending()
		log.CtxWarn(task.ctx, "dispatch queue full, drop realtime task: source_instance=%s", c.currentInstanceId)
		if c.runtimeStats() != nil {
			c.runtimeStats().IncDispatchDropped()
			c.runtimeStats().SetDispatchQueueDepth(len(c.dispatchQueue))
		}
	}
}

func (c *PushCoordinator) dispatch(ctx context.Context, msg *entity.Message, userIds []string, excludeConnId string) {
	payload := &PushPayload{Message: pushMessageFromEntity(msg)}

	localRefs := flattenConnRefs(c.local.LocalConnRefs(userIds, excludeConnId))
	if len(localRefs) > 0 {
		_ = c.local.DeliverLocal(ctx, localRefs, payload)
	}

	if c.routes == nil || c.bus == nil {
		return
	}

	routeRefs, err := c.routes.GetUsersConnRefs(ctx, userIds)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			log.CtxDebug(ctx, "route read canceled, skip remote delivery: %v", err)
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			log.CtxDebug(ctx, "route read canceled, skip remote delivery: %v", err)
			return
		}
		if c.runtimeStats() != nil {
			c.runtimeStats().IncRouteReadErrors()
		}
		c.mu.Lock()
		handler := c.onRouteReadFailure
		c.mu.Unlock()
		if handler != nil {
			handler(err)
		}
		log.CtxWarn(ctx, "route read failed, remote delivery skipped: %v", err)
		return
	}

	byInstance := make(map[string][]ConnRef)
	for _, conns := range routeRefs {
		for _, conn := range conns {
			if excludeConnId != "" && conn.ConnId == excludeConnId {
				continue
			}
			if conn.InstanceId == c.currentInstanceId {
				continue
			}
			byInstance[conn.InstanceId] = append(byInstance[conn.InstanceId], ConnRef{
				UserId:     conn.UserId,
				ConnId:     conn.ConnId,
				PlatformId: conn.PlatformId,
			})
		}
	}

	for instanceId, refs := range byInstance {
		if len(refs) == 0 {
			continue
		}
		for _, chunk := range chunkConnRefs(refs, c.maxRefsPerEnvelopeValue()) {
			env := &PushEnvelope{
				Version:        1,
				PushId:         c.newPushId(),
				SourceInstance: c.currentInstanceId,
				SentAt:         c.now().UnixMilli(),
				TargetConnMap: map[string][]ConnRef{
					instanceId: chunk,
				},
				Payload: payload,
			}
			if err := c.bus.PublishToInstance(ctx, instanceId, env); err != nil {
				log.CtxWarn(ctx, "publish failed: instance_id=%s err=%v", instanceId, err)
			}
		}
	}
}

func (c *PushCoordinator) addPending() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending++
}

func (c *PushCoordinator) ensureWorkers() {
	c.startOnce.Do(func() {
		c.mu.Lock()
		queueSize := c.queueSize
		workerNum := c.workerNum
		c.dispatchQueue = make(chan dispatchTask, queueSize)
		c.remoteQueue = make(chan *PushEnvelope, queueSize)
		stats := c.stats
		maxRefsPerEnvelope := c.maxRefsPerEnvelope
		c.mu.Unlock()

		if stats != nil {
			stats.SetDispatchQueueDepth(0)
		}
		log.CtxInfo(
			context.Background(),
			"push coordinator workers started: instance_id=%s dispatch_queue_size=%d remote_queue_size=%d worker_num=%d max_refs_per_envelope=%d",
			c.currentInstanceId, queueSize, queueSize, workerNum, maxRefsPerEnvelope,
		)
		for i := 0; i < workerNum; i++ {
			go c.dispatchWorker()
			go c.remoteWorker()
		}
	})
}

func (c *PushCoordinator) currentDispatchContext() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dispatchCtx == nil {
		return context.Background()
	}
	return c.dispatchCtx
}

func (c *PushCoordinator) runtimeStats() *RuntimeStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

func (c *PushCoordinator) maxRefsPerEnvelopeValue() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.maxRefsPerEnvelope <= 0 {
		return 500
	}
	return c.maxRefsPerEnvelope
}

func (c *PushCoordinator) updateDispatchQueueDepth() {
	stats := c.runtimeStats()
	if stats == nil || c.dispatchQueue == nil {
		return
	}
	stats.SetDispatchQueueDepth(len(c.dispatchQueue))
}

func (c *PushCoordinator) donePending() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending > 0 {
		c.pending--
	}
	c.cond.Broadcast()
}

func (c *PushCoordinator) dispatchWorker() {
	for {
		select {
		case <-c.dispatchCtx.Done():
			c.releaseQueuedTasks()
			return
		case task := <-c.dispatchQueue:
			c.updateDispatchQueueDepth()
			if c.dispatchCtx.Err() != nil {
				c.donePending()
				c.releaseQueuedTasks()
				return
			}
			c.handleDispatchTask(task)
			c.donePending()
		}
	}
}

func (c *PushCoordinator) handleDispatchTask(task dispatchTask) {
	if task.msg != nil {
		c.dispatch(task.ctx, task.msg, task.userIds, task.excludeConnId)
	}
}

func (c *PushCoordinator) remoteWorker() {
	for {
		select {
		case <-c.dispatchCtx.Done():
			c.releaseRemoteQueue()
			return
		case env := <-c.remoteQueue:
			if c.dispatchCtx.Err() != nil {
				c.releaseRemoteQueue()
				return
			}
			c.HandleRemoteEnvelope(c.currentDispatchContext(), env)
		}
	}
}

func (c *PushCoordinator) releaseQueuedTasks() {
	for {
		select {
		case <-c.dispatchQueue:
			c.updateDispatchQueueDepth()
			c.donePending()
		default:
			return
		}
	}
}

func (c *PushCoordinator) releaseRemoteQueue() {
	for {
		select {
		case <-c.remoteQueue:
		default:
			return
		}
	}
}

func (c *PushCoordinator) triggerRemoteOverflow() {
	c.remoteOverflowOnce.Do(func() {
		c.mu.Lock()
		handler := c.onRemoteOverflow
		c.mu.Unlock()
		if handler != nil {
			handler()
		}
	})
}

func flattenConnRefs(input map[string][]ConnRef) []ConnRef {
	var result []ConnRef
	for _, refs := range input {
		result = append(result, refs...)
	}
	return result
}

func chunkConnRefs(refs []ConnRef, size int) [][]ConnRef {
	if len(refs) == 0 {
		return nil
	}
	if size <= 0 || len(refs) <= size {
		return [][]ConnRef{append([]ConnRef(nil), refs...)}
	}

	chunks := make([][]ConnRef, 0, (len(refs)+size-1)/size)
	for start := 0; start < len(refs); start += size {
		end := start + size
		if end > len(refs) {
			end = len(refs)
		}
		chunks = append(chunks, append([]ConnRef(nil), refs[start:end]...))
	}
	return chunks
}

func pushMessageFromEntity(msg *entity.Message) *PushMessage {
	if msg == nil {
		return nil
	}
	content := msg.GetContent()
	pushMsg := &PushMessage{
		ServerMsgId:    msg.Id,
		ConversationId: msg.ConversationId,
		Seq:            msg.Seq,
		ClientMsgId:    msg.ClientMsgId,
		SenderId:       msg.SenderId,
		RecvId:         msg.RecvId,
		GroupId:        msg.GroupId,
		SessionType:    msg.SessionType,
		MsgType:        msg.MsgType,
		SendAt:         msg.SendAt,
	}
	pushMsg.Content.Text = content.Text
	pushMsg.Content.Image = content.Image
	pushMsg.Content.Video = content.Video
	pushMsg.Content.Audio = content.Audio
	pushMsg.Content.File = content.File
	pushMsg.Content.Custom = content.Custom
	return pushMsg
}
