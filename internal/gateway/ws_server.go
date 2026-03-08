package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/mbeoliero/kit/log"
	"github.com/redis/go-redis/v9"

	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/mbeoliero/nexo/internal/middleware"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/constant"
	"github.com/mbeoliero/nexo/pkg/errcode"
)

type routeWriteKind int

const (
	routeWriteRegister routeWriteKind = iota
	routeWriteUnregister
)

// PushTask represents a message push task.
type PushTask struct {
	Msg       *entity.Message
	TargetIds []string
	ExcludeId string
}

type routeWriteEvent struct {
	kind routeWriteKind
	conn RouteConn
}

type projectedInstanceState struct {
	pending    bool
	snapshot   LifecycleSnapshot
	generation uint64
	firstReady bool
}

// OnlineStatusResult represents a user's online status.
type OnlineStatusResult struct {
	UserId               string                  `json:"user_id"`
	Status               int                     `json:"status"`
	DetailPlatformStatus []*PlatformStatusDetail `json:"detail_platform_status,omitempty"`
}

// PlatformStatusDetail represents online status detail for a specific platform.
type PlatformStatusDetail struct {
	PlatformId   int    `json:"platform_id"`
	PlatformName string `json:"platform_name"`
	ConnId       string `json:"conn_id"`
}

// OnlineStatusMeta is the degraded online-status response meta.
type OnlineStatusMeta struct {
	Partial    bool   `json:"partial"`
	DataSource string `json:"data_source"`
}

// WsServer is the WebSocket and push orchestration server.
type WsServer struct {
	upgrader              *websocket.Upgrader
	cfg                   *config.Config
	userMap               *UserMap
	registerChan          chan *Client
	unregisterChan        chan *Client
	pushChan              chan *PushTask
	remoteEnvChan         chan *PushEnvelope
	routeWriteChan        chan routeWriteEvent
	routeRepairWake       chan struct{}
	routeRecoverWake      chan struct{}
	broadcastRecoverWake  chan struct{}
	instanceStateSyncWake chan struct{}

	msgService  *service.MessageService
	convService *service.ConversationService

	onlineUserNum atomic.Int64
	onlineConnNum atomic.Int64
	maxConnNum    int64

	gate *LocalLifecycleGate

	internalCtx    context.Context
	internalCancel context.CancelFunc
	wg             sync.WaitGroup
	drainOnce      sync.Once
	shutdownOnce   sync.Once
	drainRequests  chan DrainReason

	onlineStateWriter *OnlineStateWriter
	routeStore        *RouteStore
	instanceManager   *InstanceManager
	pushBus           PushBus
	selector          *HybridSelector
	crossCaps         *CrossInstanceCapabilities
	instanceId        string

	pushDedup sync.Map

	pendingRegisterMu sync.Mutex
	pendingRegister   map[string]RouteConn

	instanceStateSyncMu     sync.Mutex
	pendingProjectedStateMu sync.Mutex
	pendingProjectedState   projectedInstanceState

	inflightPush       atomic.Int64
	inflightRemoteEnv  atomic.Int64
	inflightRouteWrite atomic.Int64

	publishFailCount            atomic.Int32
	pushRouteReadFailCount      atomic.Int32
	presenceReadFailCount       atomic.Int32
	routeSubscribeFailCount     atomic.Int32
	broadcastSubscribeFailCount atomic.Int32

	waitRouteSubscribeReady  bool
	routeSubscribeReadyOnce  atomic.Bool
	routeSubscribeDegradedAt atomic.Int64
	routeStateGeneration     atomic.Uint64
	pendingStateSync         atomic.Bool
}

// NewWsServer creates a new WebSocket server.
func NewWsServer(cfg *config.Config, rdb *redis.Client, msgService *service.MessageService, convService *service.ConversationService) *WsServer {
	upgrader := &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			allowed := cfg.Server.AllowedOrigins
			if len(allowed) == 0 {
				return false
			}
			for _, o := range allowed {
				if o == "*" || strings.EqualFold(o, origin) {
					return true
				}
			}
			return false
		},
	}

	waitRouteSubscribeReady := cfg.WebSocket.CrossInstance.Enabled && rdb != nil
	gate := NewLifecycleGate()
	if waitRouteSubscribeReady {
		// Keep the instance out of ready/routeable rotation until the first
		// instance-channel subscription is actually established.
		gate = NewLifecycleGateWithState(false, false)
	}
	server := &WsServer{
		upgrader:                upgrader,
		cfg:                     cfg,
		userMap:                 NewUserMap(),
		registerChan:            make(chan *Client, 1000),
		unregisterChan:          make(chan *Client, 1000),
		pushChan:                make(chan *PushTask, cfg.WebSocket.PushChannelSize),
		remoteEnvChan:           make(chan *PushEnvelope, cfg.WebSocket.RemoteEnvChannelSize),
		routeWriteChan:          make(chan routeWriteEvent, cfg.WebSocket.RouteWriteQueueSize),
		routeRepairWake:         make(chan struct{}, 1),
		routeRecoverWake:        make(chan struct{}, 1),
		broadcastRecoverWake:    make(chan struct{}, 1),
		instanceStateSyncWake:   make(chan struct{}, 1),
		msgService:              msgService,
		convService:             convService,
		maxConnNum:              cfg.WebSocket.MaxConnNum,
		gate:                    gate,
		drainRequests:           make(chan DrainReason, 1),
		selector:                NewHybridSelector(cfg.WebSocket.Hybrid),
		crossCaps:               NewCrossInstanceCapabilities(cfg.WebSocket.CrossInstance.Enabled),
		instanceId:              cfg.WebSocket.CrossInstance.InstanceId,
		pendingRegister:         make(map[string]RouteConn),
		waitRouteSubscribeReady: waitRouteSubscribeReady,
	}

	if rdb != nil {
		server.onlineStateWriter = NewOnlineStateWriter(rdb, 60*time.Second)
	}
	if cfg.WebSocket.CrossInstance.Enabled && rdb != nil {
		server.routeStore = NewRouteStore(rdb, cfg)
		server.pushBus = NewRedisPushBus(rdb)
		server.instanceManager = NewInstanceManager(rdb, cfg.WebSocket.CrossInstance, server.instanceId, gate, server.GetOnlineConnCount)
	}

	return server
}

// Run starts the WebSocket server loops.
func (s *WsServer) Run(ctx context.Context) {
	if s.internalCtx != nil {
		return
	}

	s.internalCtx, s.internalCancel = context.WithCancel(context.Background())
	go func() {
		<-ctx.Done()
		s.requestDrain(DrainReasonManual)
	}()

	s.startLoop(s.eventLoop)
	workerNum := s.cfg.WebSocket.PushWorkerNum
	if workerNum <= 0 {
		workerNum = 10
	}
	for i := 0; i < workerNum; i++ {
		s.startLoop(s.pushLoop)
	}
	s.startLoop(s.remoteLoop)
	s.startLoop(s.cleanupPushDedupLoop)

	if s.onlineStateWriter != nil {
		s.startLoop(func(ctx context.Context) { s.onlineStateWriter.Run(ctx) })
	}
	if s.routeStore != nil {
		workerNum := s.cfg.WebSocket.RouteWriteWorkerNum
		if workerNum <= 0 {
			workerNum = 1
		}
		for i := 0; i < workerNum; i++ {
			s.startLoop(s.routeWriteLoop)
		}
		s.startLoop(s.routeRepairLoop)
		s.startLoop(s.routeTTLRefreshLoop)
		s.startLoop(s.routeReconcileLoop)
	}
	if s.instanceManager != nil {
		s.startLoop(s.instanceStateSyncLoop)
		s.startLoop(func(ctx context.Context) { s.instanceManager.Run(ctx) })
	}

	if s.pushBus != nil {
		s.startLoop(s.routeSubscribeRecoverLoop)
		s.startLoop(s.broadcastSubscribeRecoverLoop)
		s.wakeRouteSubscribeRecover()
		s.wakeBroadcastSubscribeRecover()
	}

	log.Info("started %d push workers", workerNum)
}

// LifecycleGate exposes the shared lifecycle gate for HTTP/WS handlers.
func (s *WsServer) LifecycleGate() LifecycleGate {
	return s.gate
}

// DrainRequests returns the internal drain request channel.
func (s *WsServer) DrainRequests() <-chan DrainReason {
	return s.drainRequests
}

// BeginDrain flips the lifecycle gate into drain mode.
func (s *WsServer) BeginDrain(reason DrainReason) {
	s.beginDrain(reason)
}

// IsReady reports whether the instance still accepts new traffic.
func (s *WsServer) IsReady() bool {
	return s.gate.IsReady()
}

// Shutdown drains the server and stops all loops.
func (s *WsServer) Shutdown(ctx context.Context, reason DrainReason) error {
	var shutdownErr error
	s.shutdownOnce.Do(func() {
		s.beginDrain(reason)

		if err := s.waitForInflightSend(ctx); err != nil {
			shutdownErr = err
			return
		}

		s.gate.CloseSendPath()
		s.syncInstanceState(ctx)

		s.drainLocalClients(ctx, reason)

		_ = s.waitForQueues(ctx)

		if s.instanceManager != nil {
			_ = s.instanceManager.Remove(ctx)
		}
		if s.internalCancel != nil {
			s.internalCancel()
		}
		if s.pushBus != nil {
			_ = s.pushBus.Close()
		}

		done := make(chan struct{})
		go func() {
			s.wg.Wait()
			close(done)
		}()

		select {
		case <-ctx.Done():
			shutdownErr = ctx.Err()
		case <-done:
		}
	})
	return shutdownErr
}

func (s *WsServer) startLoop(fn func(context.Context)) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		fn(s.internalCtx)
	}()
}

func (s *WsServer) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case client := <-s.registerChan:
			s.registerClient(ctx, client)
		case client := <-s.unregisterChan:
			s.unregisterClient(ctx, client)
		}
	}
}

func (s *WsServer) pushLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-s.pushChan:
			if task == nil {
				continue
			}
			s.inflightPush.Add(1)
			s.processPushTask(ctx, task)
			s.inflightPush.Add(-1)
		}
	}
}

func (s *WsServer) remoteLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case env := <-s.remoteEnvChan:
			if env == nil {
				continue
			}
			s.inflightRemoteEnv.Add(1)
			s.onRemoteEnvelope(ctx, env)
			s.inflightRemoteEnv.Add(-1)
		}
	}
}

func (s *WsServer) routeWriteLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-s.routeWriteChan:
			s.inflightRouteWrite.Add(1)
			s.handleRouteWrite(ctx, evt)
			s.inflightRouteWrite.Add(-1)
		}
	}
}

func (s *WsServer) routeRepairLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.routeRepairWake:
		}
		s.retryPendingRegisters(ctx)
	}
}

func (s *WsServer) routeSubscribeRecoverLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.routeRecoverWake:
		}

		for {
			if ctx.Err() != nil {
				return
			}
			if err := s.pushBus.SubscribeInstance(ctx, s.instanceId, s.enqueueRemoteEnvelope, s.onRouteSubscribeFailure); err == nil {
				s.onRouteSubscribeSuccess()
				break
			} else {
				s.handleRouteSubscribeFailure(err, false)
			}

			timer := time.NewTimer(s.cfg.WebSocket.CrossInstance.RecoverProbeInterval())
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

func (s *WsServer) broadcastSubscribeRecoverLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.broadcastRecoverWake:
		}

		for {
			if ctx.Err() != nil {
				return
			}
			if err := s.pushBus.SubscribeBroadcast(ctx, s.enqueueRemoteEnvelope, s.onBroadcastSubscribeFailure); err == nil {
				s.onBroadcastSubscribeSuccess()
				break
			} else {
				s.handleBroadcastSubscribeFailure(err, false)
			}

			timer := time.NewTimer(s.cfg.WebSocket.CrossInstance.RecoverProbeInterval())
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

func (s *WsServer) instanceStateSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.WebSocket.CrossInstance.RecoverProbeInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.instanceStateSyncWake:
		}

		s.retryPendingInstanceStateSync(ctx)
	}
}

func (s *WsServer) routeTTLRefreshLoop(ctx context.Context) {
	if s.routeStore == nil {
		return
	}

	ticker := time.NewTicker(s.cfg.WebSocket.RouteTTLRefreshInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.routeStore.RefreshInstanceRoutesTTL(ctx, s.instanceId, s.userMap.SnapshotRouteConns(s.instanceId))
		}
	}
}

func (s *WsServer) routeReconcileLoop(ctx context.Context) {
	if s.routeStore == nil {
		return
	}

	ticker := time.NewTicker(s.cfg.WebSocket.RouteReconcileInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.routeStore.ReconcileInstanceRoutes(ctx, s.instanceId, s.userMap.SnapshotRouteConns(s.instanceId))
		}
	}
}

func (s *WsServer) cleanupPushDedupLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.WebSocket.PushDedupTTL())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deadline := time.Now().Add(-s.cfg.WebSocket.PushDedupTTL()).UnixMilli()
			s.pushDedup.Range(func(key, value any) bool {
				ts, ok := value.(int64)
				if ok && ts < deadline {
					s.pushDedup.Delete(key)
				}
				return true
			})
		}
	}
}

func (s *WsServer) registerClient(ctx context.Context, client *Client) {
	existingClients, exists := s.userMap.GetAll(client.UserId)
	if !exists {
		s.onlineUserNum.Add(1)
	}

	s.userMap.Register(ctx, client)
	s.onlineConnNum.Add(1)
	if s.onlineStateWriter != nil {
		s.onlineStateWriter.MarkOnline(client.UserId)
	}
	s.enqueueRouteRegister(routeConnFromClient(client, s.instanceId))
	s.syncInstanceState(ctx)

	log.CtxInfo(ctx, "client registered: user_id=%s, platform_id=%d, conn_id=%s, existing_conns=%d, online_users=%d, online_conns=%d",
		client.UserId, client.PlatformId, client.ConnId, len(existingClients), s.onlineUserNum.Load(), s.onlineConnNum.Load())
}

func (s *WsServer) unregisterClient(ctx context.Context, client *Client) {
	isUserOffline := s.userMap.Unregister(ctx, client)
	s.onlineConnNum.Add(-1)
	if isUserOffline {
		s.onlineUserNum.Add(-1)
		if s.onlineStateWriter != nil {
			s.onlineStateWriter.MarkOffline(client.UserId)
		}
	}
	s.enqueueRouteUnregister(routeConnFromClient(client, s.instanceId))
	s.syncInstanceState(ctx)

	log.CtxInfo(ctx, "client unregistered: user_id=%s, platform_id=%d, conn_id=%s, user_offline=%v, online_users=%d, online_conns=%d",
		client.UserId, client.PlatformId, client.ConnId, isUserOffline, s.onlineUserNum.Load(), s.onlineConnNum.Load())
}

// UnregisterClient queues client for unregistration.
func (s *WsServer) UnregisterClient(client *Client) {
	if s.gate.Snapshot().Draining {
		log.Warn("server draining, wait for unregister enqueue: user_id=%s, conn_id=%s", client.UserId, client.ConnId)
		for {
			select {
			case s.unregisterChan <- client:
				return
			case <-contextDone(s.internalCtx):
				log.Warn("skip unregister after shutdown context closed: user_id=%s, conn_id=%s", client.UserId, client.ConnId)
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}

	select {
	case s.unregisterChan <- client:
	default:
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case s.unregisterChan <- client:
		case <-timer.C:
			log.Warn("unregister channel full: user_id=%s, conn_id=%s", client.UserId, client.ConnId)
		case <-s.internalCtx.Done():
		}
	}
}

// HandleConnection handles a new WebSocket connection.
func (s *WsServer) HandleConnection(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if !s.gate.CanAcceptIngress() {
		http.Error(w, errcode.ErrServerShuttingDown.Msg, http.StatusServiceUnavailable)
		return
	}
	if s.onlineConnNum.Load() >= s.maxConnNum {
		http.Error(w, "connection limit exceeded", http.StatusServiceUnavailable)
		return
	}

	token := r.URL.Query().Get(QueryToken)
	sendId := r.URL.Query().Get(QuerySendId)
	platformIdStr := r.URL.Query().Get(QueryPlatformId)
	sdkType := r.URL.Query().Get(QuerySDKType)

	if token == "" || sendId == "" {
		http.Error(w, "missing required parameters", http.StatusBadRequest)
		return
	}

	claims, err := middleware.ParseTokenWithFallback(token, s.cfg)
	if err != nil {
		log.CtxDebug(ctx, "token validation failed: send_id=%s, error=%v", sendId, err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if platformIdStr != "" {
		if pid, parseErr := strconv.Atoi(platformIdStr); parseErr == nil {
			claims.PlatformId = pid
		}
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.CtxWarn(ctx, "websocket upgrade failed: %v", err)
		return
	}

	wsConn := NewWebSocketClientConn(
		conn,
		s.cfg.WebSocket.MaxMessageSize,
		s.cfg.WebSocket.WriteWait,
		s.cfg.WebSocket.PongWait,
		s.cfg.WebSocket.PingPeriod,
		s.cfg.WebSocket.WriteChannelSize,
	)
	client := NewClient(wsConn, claims.UserId, claims.PlatformId, sdkType, token, uuid.New().String(), s)

	select {
	case s.registerChan <- client:
		client.Start()
	default:
		log.Warn("register channel full: user_id=%s", claims.UserId)
		_ = client.Close()
	}
}

// AsyncPushToUsers queues a message push to users.
func (s *WsServer) AsyncPushToUsers(msg *entity.Message, userIds []string, excludeConnId string) {
	if s.gate.Snapshot().SendClosed {
		log.Warn("send path closed, push dropped: conversation_id=%s, seq=%d", msg.ConversationId, msg.Seq)
		return
	}

	task := &PushTask{
		Msg:       msg,
		TargetIds: userIds,
		ExcludeId: excludeConnId,
	}

	select {
	case s.pushChan <- task:
	default:
		log.Warn("push channel full, message dropped: conversation_id=%s, seq=%d", msg.ConversationId, msg.Seq)
	}
}

// GetOnlineUserCount returns online user count.
func (s *WsServer) GetOnlineUserCount() int64 {
	return s.onlineUserNum.Load()
}

// GetOnlineConnCount returns online connection count.
func (s *WsServer) GetOnlineConnCount() int64 {
	return s.onlineConnNum.Load()
}

// GetUsersOnlineStatus returns online status for the given user IDs.
func (s *WsServer) GetUsersOnlineStatus(ctx context.Context, userIds []string) ([]*OnlineStatusResult, *OnlineStatusMeta) {
	results := make([]*OnlineStatusResult, 0, len(userIds))
	index := make(map[string]*OnlineStatusResult, len(userIds))

	for _, userId := range userIds {
		result := &OnlineStatusResult{
			UserId: userId,
			Status: constant.StatusOffline,
		}
		if clients, ok := s.userMap.GetAll(userId); ok && len(clients) > 0 {
			result.Status = constant.StatusOnline
			for _, client := range clients {
				result.DetailPlatformStatus = append(result.DetailPlatformStatus, &PlatformStatusDetail{
					PlatformId:   client.PlatformId,
					PlatformName: constant.PlatformIdToName(client.PlatformId),
					ConnId:       client.ConnId,
				})
			}
		}
		results = append(results, result)
		index[userId] = result
	}

	if s.routeStore == nil || !s.cfg.WebSocket.CrossInstance.Enabled {
		return results, nil
	}
	if !s.crossCaps.CanReadPresence() {
		return results, &OnlineStatusMeta{Partial: true, DataSource: "local_only"}
	}

	routeMap, err := s.routeStore.GetUsersPresenceConnRefs(ctx, userIds)
	if err != nil {
		s.onPresenceReadFailure(err)
		return results, &OnlineStatusMeta{Partial: true, DataSource: "local_only"}
	}
	s.onPresenceReadSuccess()

	for userId, refs := range routeMap {
		result := index[userId]
		for _, ref := range refs {
			if ref.InstanceId == s.instanceId {
				continue
			}
			result.Status = constant.StatusOnline
			result.DetailPlatformStatus = append(result.DetailPlatformStatus, &PlatformStatusDetail{
				PlatformId:   ref.PlatformId,
				PlatformName: constant.PlatformIdToName(ref.PlatformId),
				ConnId:       ref.ConnId,
			})
		}
	}

	return results, nil
}

func (s *WsServer) messageToMsgData(msg *entity.Message) *MessageData {
	return s.payloadToMessageData(newPushPayload(msg))
}

func (s *WsServer) payloadToMessageData(payload *PushPayload) *MessageData {
	return &MessageData{
		ServerMsgId:    payload.MsgId,
		ConversationId: payload.ConversationId,
		Seq:            payload.Seq,
		ClientMsgId:    payload.ClientMsgId,
		SenderId:       payload.SenderId,
		RecvId:         payload.RecvId,
		GroupId:        payload.GroupId,
		SessionType:    payload.SessionType,
		MsgType:        payload.MsgType,
		Content: struct {
			Text   string `json:"text,omitempty"`
			Image  string `json:"image,omitempty"`
			Video  string `json:"video,omitempty"`
			Audio  string `json:"audio,omitempty"`
			File   string `json:"file,omitempty"`
			Custom string `json:"custom,omitempty"`
		}{
			Text:   payload.Content.Text,
			Image:  payload.Content.Image,
			Video:  payload.Content.Video,
			Audio:  payload.Content.Audio,
			File:   payload.Content.File,
			Custom: payload.Content.Custom,
		},
		SendAt: payload.SendAt,
	}
}

// HandleGetNewestSeq handles get newest seq request.
func (s *WsServer) HandleGetNewestSeq(ctx context.Context, client *Client, req *WSRequest) ([]byte, error) {
	var getSeqReq GetNewestSeqReq
	if err := json.Unmarshal(req.Data, &getSeqReq); err != nil {
		return nil, errcode.ErrInvalidParam
	}

	seqs := make(map[string]int64)
	for _, convId := range getSeqReq.ConversationIds {
		maxSeq, _ := s.msgService.GetMaxSeq(ctx, client.UserId, convId)
		seqs[convId] = maxSeq
	}

	resp := GetNewestSeqResp{Seqs: seqs}
	return json.Marshal(resp)
}

// HandleSendMsg handles send message request.
func (s *WsServer) HandleSendMsg(ctx context.Context, client *Client, req *WSRequest) ([]byte, error) {
	var sendReq SendMsgReq
	if err := json.Unmarshal(req.Data, &sendReq); err != nil {
		return nil, errcode.ErrInvalidParam
	}

	release, err := s.gate.AcquireSendLease()
	if err != nil {
		return nil, err
	}
	defer release()

	svcReq := &service.SendMessageRequest{
		ClientMsgId: sendReq.ClientMsgId,
		RecvId:      sendReq.RecvId,
		GroupId:     sendReq.GroupId,
		SessionType: sendReq.SessionType,
		MsgType:     sendReq.MsgType,
		Content: entity.MessageContent{
			Text:   sendReq.Content.Text,
			Image:  sendReq.Content.Image,
			Video:  sendReq.Content.Video,
			Audio:  sendReq.Content.Audio,
			File:   sendReq.Content.File,
			Custom: sendReq.Content.Custom,
		},
	}

	msg, err := s.msgService.SendMessage(ctx, client.UserId, svcReq)
	if err != nil {
		return nil, err
	}

	resp := SendMsgResp{
		ServerMsgId:    msg.Id,
		ConversationId: msg.ConversationId,
		Seq:            msg.Seq,
		ClientMsgId:    msg.ClientMsgId,
		SendAt:         msg.SendAt,
	}

	return json.Marshal(resp)
}

// HandlePullMsg handles pull messages request.
func (s *WsServer) HandlePullMsg(ctx context.Context, client *Client, req *WSRequest) ([]byte, error) {
	var pullReq PullMsgReq
	if err := json.Unmarshal(req.Data, &pullReq); err != nil {
		return nil, errcode.ErrInvalidParam
	}

	svcReq := &service.PullMessagesRequest{
		ConversationId: pullReq.ConversationId,
		BeginSeq:       pullReq.BeginSeq,
		EndSeq:         pullReq.EndSeq,
		Limit:          pullReq.Limit,
	}

	messages, maxSeq, err := s.msgService.PullMessages(ctx, client.UserId, svcReq)
	if err != nil {
		return nil, err
	}

	msgDataList := make([]*MessageData, 0, len(messages))
	for _, msg := range messages {
		msgDataList = append(msgDataList, s.messageToMsgData(msg))
	}

	resp := PullMsgResp{
		Messages: msgDataList,
		MaxSeq:   maxSeq,
	}

	return json.Marshal(resp)
}

// HandlePullMsgBySeqList handles pull messages by seq list request.
func (s *WsServer) HandlePullMsgBySeqList(ctx context.Context, client *Client, req *WSRequest) ([]byte, error) {
	return s.HandlePullMsg(ctx, client, req)
}

// HandleGetConvMaxReadSeq handles get conversation max/read seq request.
func (s *WsServer) HandleGetConvMaxReadSeq(ctx context.Context, client *Client, req *WSRequest) ([]byte, error) {
	var getSeqReq GetConvMaxReadSeqReq
	if err := json.Unmarshal(req.Data, &getSeqReq); err != nil {
		return nil, errcode.ErrInvalidParam
	}

	maxSeq, readSeq, err := s.convService.GetMaxReadSeq(ctx, client.UserId, getSeqReq.ConversationId)
	if err != nil {
		return nil, err
	}

	unreadCount := maxSeq - readSeq
	if unreadCount < 0 {
		unreadCount = 0
	}

	resp := GetConvMaxReadSeqResp{
		MaxSeq:      maxSeq,
		ReadSeq:     readSeq,
		UnreadCount: unreadCount,
	}

	return json.Marshal(resp)
}

func (s *WsServer) processPushTask(ctx context.Context, task *PushTask) {
	payload := newPushPayload(task.Msg)
	msgData := s.payloadToMessageData(payload)

	s.pushToLocalClients(ctx, task, msgData)

	if !s.crossCaps.CanPublishOutbound() || s.pushBus == nil {
		return
	}

	s.dispatchCrossInstance(ctx, task, payload)
}

func (s *WsServer) pushToLocalClients(ctx context.Context, task *PushTask, msgData *MessageData) {
	for _, userId := range task.TargetIds {
		clients, ok := s.userMap.GetAll(userId)
		if !ok {
			continue
		}
		for _, client := range clients {
			if task.ExcludeId != "" && client.ConnId == task.ExcludeId {
				continue
			}
			if err := client.PushMessage(ctx, msgData); err != nil && err != ErrWriteChannelFull {
				log.CtxDebug(ctx, "push to client failed: user_id=%s, conn_id=%s, error=%v", userId, client.ConnId, err)
			}
		}
	}
}

func (s *WsServer) dispatchCrossInstance(ctx context.Context, task *PushTask, payload *PushPayload) {
	if s.routeStore == nil || !s.crossCaps.CanReadPushRoute() {
		s.handlePushRouteUnavailable(ctx, task, nil)
		return
	}

	routeMap, err := s.routeStore.GetUsersConnRefs(ctx, task.TargetIds)
	if err != nil {
		s.onPushRouteReadFailure(err)
		s.handlePushRouteUnavailable(ctx, task, err)
		return
	}
	s.onPushRouteReadSuccess()

	remoteRouteMap := filterOutLocalInstance(routeMap, s.instanceId)
	if len(remoteRouteMap) == 0 {
		return
	}

	targetUsers := uniqueUsers(remoteRouteMap)
	instanceIds := uniqueInstances(remoteRouteMap)
	mode := s.selector.Select(task.Msg.SessionType, len(targetUsers), len(instanceIds))
	if mode == PushModeBroadcast && (!s.crossCaps.CanReceiveBroadcastEnvelope() || s.instanceManager == nil || !s.instanceManager.AllBroadcastReady(ctx, instanceIds)) {
		mode = PushModeRoute
	}

	switch mode {
	case PushModeBroadcast:
		s.publishBroadcastEnvelopes(ctx, payload, targetUsers)
	default:
		s.publishRouteEnvelopes(ctx, payload, remoteRouteMap)
	}
}

func (s *WsServer) handlePushRouteUnavailable(ctx context.Context, task *PushTask, err error) {
	if err != nil {
		log.CtxWarn(ctx, "push route unavailable, degrade to local-only: conversation_id=%s, seq=%d, err=%v", task.Msg.ConversationId, task.Msg.Seq, err)
	}
}

func (s *WsServer) publishRouteEnvelopes(ctx context.Context, payload *PushPayload, routeMap map[string][]RouteConn) {
	grouped := groupByInstance(routeMap)
	for instanceId, refs := range grouped {
		env := &PushEnvelope{
			PushId:         uuid.New().String(),
			Mode:           PushModeRoute,
			TargetConnMap:  map[string][]ConnRef{instanceId: refs},
			SourceInstance: s.instanceId,
			SentAt:         time.Now().UnixMilli(),
			Payload:        payload,
		}
		if err := s.pushBus.PublishToInstance(ctx, instanceId, env); err != nil {
			s.onPublishFailure(err)
			continue
		}
		s.onPublishSuccess()
	}
}

func (s *WsServer) publishBroadcastEnvelopes(ctx context.Context, payload *PushPayload, targetUsers []string) {
	chunkPushId := uuid.New().String()
	chunks := splitTargetUserIdsByMarshalSize(targetUsers, payload, s.cfg.WebSocket.MaxBroadcastChunkBytes)
	for idx, chunk := range chunks {
		env := &PushEnvelope{
			PushId:         uuid.New().String(),
			ChunkPushId:    chunkPushId,
			ChunkIndex:     idx + 1,
			ChunkTotal:     len(chunks),
			Mode:           PushModeBroadcast,
			TargetUserIds:  chunk,
			SourceInstance: s.instanceId,
			SentAt:         time.Now().UnixMilli(),
			Payload:        payload,
		}
		if err := s.pushBus.PublishBroadcast(ctx, env); err != nil {
			s.onPublishFailure(err)
			continue
		}
		s.onPublishSuccess()
	}
}

func (s *WsServer) enqueueRemoteEnvelope(ctx context.Context, env *PushEnvelope) {
	select {
	case s.remoteEnvChan <- env:
	default:
		timer := time.NewTimer(s.cfg.WebSocket.RemoteEnvBlockTimeout())
		defer timer.Stop()
		select {
		case s.remoteEnvChan <- env:
		case <-timer.C:
			log.CtxWarn(ctx, "remote env channel full, drop push_id=%s", env.PushId)
		case <-s.internalCtx.Done():
		}
	}
}

func (s *WsServer) onRemoteEnvelope(ctx context.Context, env *PushEnvelope) {
	if env.Mode == PushModeBroadcast && env.SourceInstance == s.instanceId {
		return
	}
	if _, loaded := s.pushDedup.LoadOrStore(env.PushId, time.Now().UnixMilli()); loaded {
		return
	}

	msgData := s.payloadToMessageData(env.Payload)
	switch env.Mode {
	case PushModeRoute:
		for _, ref := range env.TargetConnMap[s.instanceId] {
			clients, ok := s.userMap.GetAll(ref.UserId)
			if !ok {
				continue
			}
			for _, client := range clients {
				if client.ConnId != ref.ConnId {
					continue
				}
				_ = client.PushMessage(ctx, msgData)
			}
		}
	case PushModeBroadcast:
		for _, userId := range env.TargetUserIds {
			clients, ok := s.userMap.GetAll(userId)
			if !ok {
				continue
			}
			for _, client := range clients {
				_ = client.PushMessage(ctx, msgData)
			}
		}
	}
}

func (s *WsServer) handleRouteWrite(ctx context.Context, evt routeWriteEvent) {
	if s.routeStore == nil {
		return
	}

	switch evt.kind {
	case routeWriteRegister:
		if err := s.routeStore.RegisterConn(ctx, evt.conn); err != nil {
			s.savePendingRegister(evt.conn)
			s.wakeRouteRepair()
		}
	case routeWriteUnregister:
		_ = s.routeStore.UnregisterConn(ctx, evt.conn)
	}
}

func (s *WsServer) enqueueRouteRegister(conn RouteConn) {
	if s.routeStore == nil || conn.InstanceId == "" {
		return
	}
	select {
	case s.routeWriteChan <- routeWriteEvent{kind: routeWriteRegister, conn: conn}:
	default:
		s.savePendingRegister(conn)
		s.wakeRouteRepair()
	}
}

func (s *WsServer) enqueueRouteUnregister(conn RouteConn) {
	if s.routeStore == nil || conn.InstanceId == "" {
		return
	}
	select {
	case s.routeWriteChan <- routeWriteEvent{kind: routeWriteUnregister, conn: conn}:
	default:
		log.Warn("route unregister queue full: user_id=%s, conn_id=%s", conn.UserId, conn.ConnId)
	}
}

func (s *WsServer) savePendingRegister(conn RouteConn) {
	s.pendingRegisterMu.Lock()
	defer s.pendingRegisterMu.Unlock()
	s.pendingRegister[routeConnKey(conn)] = conn
}

func (s *WsServer) retryPendingRegisters(ctx context.Context) {
	if s.routeStore == nil {
		return
	}

	s.pendingRegisterMu.Lock()
	pending := make([]RouteConn, 0, len(s.pendingRegister))
	for _, conn := range s.pendingRegister {
		pending = append(pending, conn)
	}
	s.pendingRegisterMu.Unlock()

	for _, conn := range pending {
		retryCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		err := s.routeStore.RegisterConn(retryCtx, conn)
		cancel()
		if err != nil {
			continue
		}
		s.pendingRegisterMu.Lock()
		delete(s.pendingRegister, routeConnKey(conn))
		s.pendingRegisterMu.Unlock()
	}
}

func (s *WsServer) wakeRouteRepair() {
	select {
	case s.routeRepairWake <- struct{}{}:
	default:
	}
}

func (s *WsServer) beginDrain(reason DrainReason) {
	s.drainOnce.Do(func() {
		s.routeStateGeneration.Add(1)
		s.clearPendingProjectedState()
		s.gate.MarkUnready()
		if reason == DrainReasonSubscribeFault {
			s.gate.MarkUnrouteable()
		}
		s.gate.CloseIngress()
		s.gate.BeginSendDrain()
		s.gate.EnterDraining()
		s.syncInstanceState(context.Background())
	})
}

func (s *WsServer) requestDrain(reason DrainReason) {
	s.beginDrain(reason)
	select {
	case s.drainRequests <- reason:
	default:
	}
}

func (s *WsServer) waitForInflightSend(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.gate.Snapshot().InflightSend == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *WsServer) waitForQueues(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if len(s.pushChan) == 0 &&
			len(s.remoteEnvChan) == 0 &&
			len(s.routeWriteChan) == 0 &&
			len(s.registerChan) == 0 &&
			len(s.unregisterChan) == 0 &&
			s.inflightPush.Load() == 0 &&
			s.inflightRemoteEnv.Load() == 0 &&
			s.inflightRouteWrite.Load() == 0 &&
			s.onlineConnNum.Load() == 0 {
			s.pendingRegisterMu.Lock()
			pending := len(s.pendingRegister)
			s.pendingRegisterMu.Unlock()
			if pending == 0 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *WsServer) drainLocalClients(ctx context.Context, reason DrainReason) {
	deadline := time.Now().Add(s.cfg.WebSocket.CrossInstance.DrainTimeout())
	graceDeadline := time.Now().Add(s.cfg.WebSocket.CrossInstance.DrainRouteableGrace())

	if reason != DrainReasonSubscribeFault {
		log.Info("planned drain grace started: routeable remains enabled briefly, conn_count=%d", s.onlineConnNum.Load())
		for time.Now().Before(graceDeadline) {
			if s.onlineConnNum.Load() == 0 {
				s.gate.MarkUnrouteable()
				s.syncInstanceState(ctx)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
		s.gate.MarkUnrouteable()
		s.syncInstanceState(ctx)
		log.Info("planned drain grace finished, start kicking remaining connections: conn_count=%d", s.onlineConnNum.Load())
	} else {
		log.Warn("route subscription unavailable, start immediate route-only drain: conn_count=%d", s.onlineConnNum.Load())
	}

	clients := s.userMap.SnapshotClients()
	batchSize := s.cfg.WebSocket.CrossInstance.DrainKickBatchSize
	if batchSize <= 0 {
		batchSize = 200
	}
	interval := s.cfg.WebSocket.CrossInstance.DrainKickInterval()
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	for idx, client := range clients {
		_ = client.KickOnline()
		if (idx+1)%batchSize == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	}

	for time.Now().Before(deadline) {
		if s.onlineConnNum.Load() == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	log.Warn("drain timed out with remaining connections: conn_count=%d", s.onlineConnNum.Load())
}

func (s *WsServer) syncInstanceState(ctx context.Context) {
	if s.instanceManager == nil {
		return
	}

	s.instanceStateSyncMu.Lock()
	defer s.instanceStateSyncMu.Unlock()
	s.syncInstanceStateLocked(ctx)
}

func (s *WsServer) syncInstanceStateLocked(ctx context.Context) {
	if err := s.instanceManager.Sync(ctx); err != nil {
		s.pendingStateSync.Store(true)
		log.Warn("instance state sync failed, will retry: err=%v", err)
		s.wakeInstanceStateSync()
		return
	}
	s.pendingStateSync.Store(false)
}

func (s *WsServer) onPublishFailure(err error) {
	failures := s.publishFailCount.Add(1)
	if failures >= int32(s.cfg.WebSocket.CrossInstance.PublishFailThreshold) {
		s.crossCaps.Publish.Store(int32(CapabilityDegraded))
		log.Warn("cross publish degraded: %v", err)
	}
}

func (s *WsServer) onPublishSuccess() {
	s.publishFailCount.Store(0)
	if s.cfg.WebSocket.CrossInstance.Enabled {
		s.crossCaps.Publish.Store(int32(CapabilityReady))
	}
}

func (s *WsServer) onPushRouteReadFailure(err error) {
	failures := s.pushRouteReadFailCount.Add(1)
	if failures >= int32(s.cfg.WebSocket.CrossInstance.PushRouteFailThreshold) {
		s.crossCaps.PushRouteRead.Store(int32(CapabilityDegraded))
		log.Warn("push route read degraded: %v", err)
	}
}

func (s *WsServer) onPushRouteReadSuccess() {
	s.pushRouteReadFailCount.Store(0)
	if s.cfg.WebSocket.CrossInstance.Enabled {
		s.crossCaps.PushRouteRead.Store(int32(CapabilityReady))
	}
}

func (s *WsServer) onPresenceReadFailure(err error) {
	failures := s.presenceReadFailCount.Add(1)
	if failures >= int32(s.cfg.WebSocket.CrossInstance.PresenceReadFailThreshold) {
		s.crossCaps.PresenceRead.Store(int32(CapabilityDegraded))
		log.Warn("presence read degraded: %v", err)
	}
}

func (s *WsServer) onPresenceReadSuccess() {
	s.presenceReadFailCount.Store(0)
	if s.cfg.WebSocket.CrossInstance.Enabled {
		s.crossCaps.PresenceRead.Store(int32(CapabilityReady))
	}
}

func (s *WsServer) onRouteSubscribeFailure(err error) {
	s.handleRouteSubscribeFailure(err, true)
}

func (s *WsServer) handleRouteSubscribeFailure(err error, wakeRecover bool) {
	failures := s.routeSubscribeFailCount.Add(1)
	if failures < int32(s.cfg.WebSocket.CrossInstance.RouteSubscribeFailThreshold) {
		log.Warn("route subscribe transient failure: consecutive=%d err=%v", failures, err)
		return
	}

	s.routeStateGeneration.Add(1)
	s.clearPendingProjectedState()
	nowMillis := time.Now().UnixMilli()
	if s.routeSubscribeDegradedAt.Load() == 0 {
		s.routeSubscribeDegradedAt.CompareAndSwap(0, nowMillis)
	}

	s.crossCaps.RouteSubscribe.Store(int32(CapabilityDegraded))

	if s.waitRouteSubscribeReady && !s.routeSubscribeReadyOnce.Load() {
		log.Warn("route subscribe bootstrap unavailable, keep instance unready until first subscribe succeeds: err=%v", err)
	} else {
		s.gate.MarkUnrouteable()
		s.syncInstanceState(context.Background())
		log.Warn("route subscribe degraded, instance stays ready but becomes temporarily unrouteable: err=%v", err)
	}

	drainAfter := s.cfg.WebSocket.CrossInstance.RouteSubscribeDrainAfter()
	degradedAt := s.routeSubscribeDegradedAt.Load()
	if drainAfter > 0 &&
		s.routeSubscribeReadyOnce.Load() &&
		!s.gate.Snapshot().Draining &&
		nowMillis-degradedAt >= drainAfter.Milliseconds() {
		log.Warn("route subscribe outage exceeded drain threshold, request drain: err=%v, degraded_for_ms=%d", err, nowMillis-degradedAt)
		s.requestDrain(DrainReasonSubscribeFault)
	}
	if wakeRecover {
		s.wakeRouteSubscribeRecover()
	}
}

func (s *WsServer) onBroadcastSubscribeFailure(err error) {
	s.handleBroadcastSubscribeFailure(err, true)
}

func (s *WsServer) handleBroadcastSubscribeFailure(err error, wakeRecover bool) {
	readinessWithdrawn := false
	if s.instanceManager != nil && s.instanceManager.broadcastReady.Load() {
		// Stop advertising broadcast eligibility on the first confirmed
		// subscription failure so peers fall back to route delivery immediately.
		s.instanceManager.SetBroadcastReady(false)
		readinessWithdrawn = true
	}
	if readinessWithdrawn || s.pendingStateSync.Load() {
		s.syncInstanceState(context.Background())
	}

	failures := s.broadcastSubscribeFailCount.Add(1)
	if failures < int32(s.cfg.WebSocket.CrossInstance.BroadcastSubscribeFailThreshold) {
		if readinessWithdrawn {
			log.Warn("broadcast subscribe transient failure, withdraw broadcast_ready immediately: consecutive=%d err=%v", failures, err)
		} else {
			log.Warn("broadcast subscribe transient failure: consecutive=%d err=%v", failures, err)
		}
		return
	}

	s.crossCaps.BroadcastSubscribe.Store(int32(CapabilityDegraded))
	log.Warn("broadcast subscribe degraded, disable broadcast temporarily: err=%v", err)
	if wakeRecover {
		s.wakeBroadcastSubscribeRecover()
	}
}

func (s *WsServer) onRouteSubscribeSuccess() {
	s.routeSubscribeFailCount.Store(0)
	s.routeSubscribeDegradedAt.Store(0)
	if s.cfg.WebSocket.CrossInstance.Enabled {
		s.crossCaps.RouteSubscribe.Store(int32(CapabilityReady))
	}

	snap := s.gate.Snapshot()
	if !snap.Draining {
		firstReady := s.waitRouteSubscribeReady && !s.routeSubscribeReadyOnce.Load() && !snap.Ready
		projected := snap
		if firstReady {
			projected.Ready = true
		}
		if snap.Ready || firstReady {
			projected.Routeable = true
		}

		if projected.Ready != snap.Ready || projected.Routeable != snap.Routeable {
			generation := s.routeStateGeneration.Load()
			if !s.syncProjectedInstanceState(context.Background(), projected, generation, firstReady) {
				if s.hasPendingProjectedState() {
					log.Warn("route subscribe recovered but mirror sync is pending before local route state opens")
				}
				return
			}
			s.applyProjectedLifecycleState(projected)
			if firstReady {
				s.routeSubscribeReadyOnce.Store(true)
				log.Info("route subscribe established, instance becomes ready")
			}
		} else {
			s.syncInstanceState(context.Background())
		}
	}
	log.Info("route subscribe recovered")
}

func (s *WsServer) onBroadcastSubscribeSuccess() {
	s.broadcastSubscribeFailCount.Store(0)
	if s.cfg.WebSocket.CrossInstance.Enabled {
		s.crossCaps.BroadcastSubscribe.Store(int32(CapabilityReady))
	}
	if s.instanceManager != nil {
		s.instanceManager.SetBroadcastReady(true)
		s.syncInstanceState(context.Background())
	}
	log.Info("broadcast subscribe recovered")
}

func (s *WsServer) wakeRouteSubscribeRecover() {
	select {
	case s.routeRecoverWake <- struct{}{}:
	default:
	}
}

func (s *WsServer) wakeBroadcastSubscribeRecover() {
	select {
	case s.broadcastRecoverWake <- struct{}{}:
	default:
	}
}

func (s *WsServer) wakeInstanceStateSync() {
	select {
	case s.instanceStateSyncWake <- struct{}{}:
	default:
	}
}

func (s *WsServer) syncProjectedInstanceState(ctx context.Context, snapshot LifecycleSnapshot, generation uint64, firstReady bool) bool {
	if s.instanceManager == nil {
		return true
	}

	s.instanceStateSyncMu.Lock()
	defer s.instanceStateSyncMu.Unlock()

	if generation != s.routeStateGeneration.Load() || s.gate.Snapshot().Draining {
		return false
	}

	if err := s.instanceManager.SyncSnapshot(ctx, snapshot); err != nil {
		s.pendingProjectedStateMu.Lock()
		s.pendingProjectedState = projectedInstanceState{
			pending:    true,
			snapshot:   snapshot,
			generation: generation,
			firstReady: firstReady,
		}
		s.pendingProjectedStateMu.Unlock()
		log.Warn("projected instance state sync failed, will retry before local state changes: err=%v", err)
		s.wakeInstanceStateSync()
		return false
	}

	if generation != s.routeStateGeneration.Load() || s.gate.Snapshot().Draining {
		return false
	}
	s.clearPendingProjectedState()
	return true
}

func (s *WsServer) applyProjectedLifecycleState(snapshot LifecycleSnapshot) {
	if snapshot.Ready {
		s.gate.MarkReady()
	}
	if snapshot.Routeable {
		s.gate.MarkRouteable()
	}
}

func (s *WsServer) retryPendingInstanceStateSync(ctx context.Context) {
	if s.instanceManager == nil {
		return
	}

	s.pendingProjectedStateMu.Lock()
	pendingProjected := s.pendingProjectedState
	s.pendingProjectedStateMu.Unlock()

	if pendingProjected.pending {
		if s.gate.Snapshot().Draining || pendingProjected.generation != s.routeStateGeneration.Load() {
			s.clearPendingProjectedState()
		} else if s.syncProjectedInstanceState(ctx, pendingProjected.snapshot, pendingProjected.generation, pendingProjected.firstReady) {
			s.applyProjectedLifecycleState(pendingProjected.snapshot)
			if pendingProjected.firstReady {
				s.routeSubscribeReadyOnce.Store(true)
				log.Info("instance mirror retry succeeded, instance becomes ready")
			} else {
				log.Info("instance mirror retry succeeded, projected route state applied")
			}
		}
	}

	if s.pendingStateSync.Load() {
		s.syncInstanceState(ctx)
	}
}

func (s *WsServer) clearPendingProjectedState() {
	s.pendingProjectedStateMu.Lock()
	s.pendingProjectedState = projectedInstanceState{}
	s.pendingProjectedStateMu.Unlock()
}

func (s *WsServer) hasPendingProjectedState() bool {
	s.pendingProjectedStateMu.Lock()
	defer s.pendingProjectedStateMu.Unlock()
	return s.pendingProjectedState.pending
}

func routeConnFromClient(client *Client, instanceId string) RouteConn {
	return RouteConn{
		UserId:     client.UserId,
		ConnId:     client.ConnId,
		InstanceId: instanceId,
		PlatformId: client.PlatformId,
	}
}

func routeConnKey(conn RouteConn) string {
	return fmt.Sprintf("%s|%s|%s", conn.UserId, conn.ConnId, conn.InstanceId)
}

func filterOutLocalInstance(routeMap map[string][]RouteConn, instanceId string) map[string][]RouteConn {
	result := make(map[string][]RouteConn, len(routeMap))
	for userId, refs := range routeMap {
		for _, ref := range refs {
			if ref.InstanceId == instanceId {
				continue
			}
			result[userId] = append(result[userId], ref)
		}
	}
	return result
}

func groupByInstance(routeMap map[string][]RouteConn) map[string][]ConnRef {
	result := make(map[string][]ConnRef)
	for _, refs := range routeMap {
		for _, ref := range refs {
			result[ref.InstanceId] = append(result[ref.InstanceId], ConnRef{
				UserId:     ref.UserId,
				ConnId:     ref.ConnId,
				PlatformId: ref.PlatformId,
			})
		}
	}
	return result
}

func uniqueUsers(routeMap map[string][]RouteConn) []string {
	users := make([]string, 0, len(routeMap))
	for userId, refs := range routeMap {
		if len(refs) == 0 {
			continue
		}
		users = append(users, userId)
	}
	return users
}

func uniqueInstances(routeMap map[string][]RouteConn) []string {
	uniq := make(map[string]struct{})
	for _, refs := range routeMap {
		for _, ref := range refs {
			uniq[ref.InstanceId] = struct{}{}
		}
	}
	result := make([]string, 0, len(uniq))
	for instanceId := range uniq {
		result = append(result, instanceId)
	}
	return result
}

func splitTargetUserIdsByMarshalSize(targetUsers []string, payload *PushPayload, maxBytes int) [][]string {
	if len(targetUsers) == 0 {
		return nil
	}
	if maxBytes <= 0 {
		return [][]string{targetUsers}
	}

	base := &PushEnvelope{
		PushId:         strings.Repeat("a", 36),
		ChunkPushId:    strings.Repeat("b", 36),
		ChunkIndex:     1,
		ChunkTotal:     1,
		Mode:           PushModeBroadcast,
		SourceInstance: "source",
		SentAt:         time.Now().UnixMilli(),
		Payload:        payload,
	}

	chunks := make([][]string, 0, 1)
	current := make([]string, 0, len(targetUsers))

	for _, userId := range targetUsers {
		next := append(append([]string(nil), current...), userId)
		base.TargetUserIds = next
		data, err := json.Marshal(base)
		if err == nil && len(data) > maxBytes && len(current) > 0 {
			chunks = append(chunks, current)
			current = []string{userId}
			continue
		}
		current = next
	}

	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

func contextDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}
