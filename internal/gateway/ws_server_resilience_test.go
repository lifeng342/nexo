package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mbeoliero/nexo/internal/config"
)

func TestDrainLocalClientsPlannedUpgradeWaitsForGraceBeforeKick(t *testing.T) {
	server := newResilienceTestServer()
	server.cfg.WebSocket.CrossInstance.DrainTimeoutSeconds = 1
	server.cfg.WebSocket.CrossInstance.DrainRouteableGraceSeconds = 1

	conn := &recordingClientConn{}
	client := NewClient(conn, "u1", 1, "go", "token", "c1", server)
	server.userMap.Register(context.Background(), client)
	server.onlineConnNum.Store(1)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()

	server.drainLocalClients(ctx, DrainReasonPlannedUpgrade)

	firstKickAt := conn.FirstControlAt()
	if firstKickAt.IsZero() {
		t.Fatal("KickOnline was never attempted")
	}
	if firstKickAt.Sub(start) < 900*time.Millisecond {
		t.Fatalf("KickOnline happened too early: %v", firstKickAt.Sub(start))
	}
}

func TestUnregisterClientRetriesWhileDraining(t *testing.T) {
	server := newResilienceTestServer()
	server.internalCtx, server.internalCancel = context.WithCancel(context.Background())
	defer server.internalCancel()

	server.gate.EnterDraining()
	server.unregisterChan <- &Client{UserId: "occupied", ConnId: "occupied"}

	client := &Client{UserId: "u1", ConnId: "c1"}
	done := make(chan struct{})
	go func() {
		server.UnregisterClient(client)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("UnregisterClient returned before queue space was available")
	default:
	}

	<-server.unregisterChan

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("UnregisterClient did not retry during draining")
	}

	select {
	case got := <-server.unregisterChan:
		if got != client {
			t.Fatalf("unexpected queued client: %+v", got)
		}
	default:
		t.Fatal("expected retried unregister to be queued")
	}
}

func TestRouteSubscribeFailureDoesNotRequestDrain(t *testing.T) {
	server := newResilienceTestServer()
	server.routeRecoverWake = make(chan struct{}, 1)

	server.onRouteSubscribeFailure(errors.New("temporary network error"))

	if !server.gate.IsReady() {
		t.Fatal("route subscribe failure should not mark instance unready")
	}
	if server.gate.IsRouteable() {
		t.Fatal("route subscribe failure should temporarily mark instance unrouteable")
	}
	if server.crossCaps.RouteSubscribe.Load() != int32(CapabilityDegraded) {
		t.Fatal("route subscribe capability should become degraded")
	}

	select {
	case reason := <-server.DrainRequests():
		t.Fatalf("unexpected drain request: %s", reason)
	default:
	}
}

func TestRouteSubscribeSuccessRestoresRouteable(t *testing.T) {
	server := newResilienceTestServer()
	server.routeRecoverWake = make(chan struct{}, 1)

	server.onRouteSubscribeFailure(errors.New("temporary network error"))
	server.onRouteSubscribeSuccess()

	if !server.gate.IsReady() {
		t.Fatal("route subscribe recovery should keep instance ready")
	}
	if !server.gate.IsRouteable() {
		t.Fatal("route subscribe recovery should restore routeable state")
	}
	if server.crossCaps.RouteSubscribe.Load() != int32(CapabilityReady) {
		t.Fatal("route subscribe capability should recover to ready")
	}
}

func TestCrossInstanceServerStartsUnreadyUntilFirstRouteSubscribe(t *testing.T) {
	cfg := &config.Config{
		WebSocket: config.WebSocketConfig{
			PushChannelSize:      1,
			RemoteEnvChannelSize: 1,
			RouteWriteQueueSize:  1,
			RouteWriteWorkerNum:  1,
			PushWorkerNum:        1,
			CrossInstance: config.CrossInstanceConfig{
				Enabled:                     true,
				InstanceId:                  "inst-a",
				RouteSubscribeFailThreshold: 1,
				RecoverProbeIntervalSeconds: 1,
			},
			Hybrid: config.HybridConfig{
				Enabled: true,
			},
		},
	}

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer func() { _ = rdb.Close() }()

	server := NewWsServer(cfg, rdb, nil, nil)
	server.instanceManager = nil
	if server.IsReady() {
		t.Fatal("cross-instance server should stay unready before first route subscribe succeeds")
	}
	if server.gate.IsRouteable() {
		t.Fatal("cross-instance server should stay unrouteable before first route subscribe succeeds")
	}

	server.onRouteSubscribeSuccess()

	if !server.IsReady() {
		t.Fatal("cross-instance server should become ready after first route subscribe succeeds")
	}
	if !server.gate.IsRouteable() {
		t.Fatal("cross-instance server should become routeable after first route subscribe succeeds")
	}
}

func TestRouteSubscribePersistentFailureRequestsDrain(t *testing.T) {
	server := newResilienceTestServer()
	server.cfg.WebSocket.CrossInstance.RouteSubscribeDrainAfterSeconds = 1
	server.routeSubscribeReadyOnce.Store(true)
	server.routeSubscribeDegradedAt.Store(time.Now().Add(-2 * time.Second).UnixMilli())

	server.onRouteSubscribeFailure(errors.New("persistent route subscribe outage"))

	if server.gate.IsReady() {
		t.Fatal("persistent route subscribe outage should stop readiness")
	}

	select {
	case reason := <-server.DrainRequests():
		if reason != DrainReasonSubscribeFault {
			t.Fatalf("unexpected drain reason: %s", reason)
		}
	default:
		t.Fatal("persistent route subscribe outage should request drain")
	}
}

func TestCrossInstanceServerStartsBroadcastNotReadyUntilFirstBroadcastSubscribe(t *testing.T) {
	cfg := &config.Config{
		WebSocket: config.WebSocketConfig{
			PushChannelSize:      1,
			RemoteEnvChannelSize: 1,
			RouteWriteQueueSize:  1,
			RouteWriteWorkerNum:  1,
			PushWorkerNum:        1,
			CrossInstance: config.CrossInstanceConfig{
				Enabled:                     true,
				InstanceId:                  "inst-a",
				RouteSubscribeFailThreshold: 1,
				RecoverProbeIntervalSeconds: 1,
			},
			Hybrid: config.HybridConfig{
				Enabled: true,
			},
		},
	}

	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer func() { _ = rdb.Close() }()

	server := NewWsServer(cfg, rdb, nil, nil)
	server.instanceManager.rdb = nil

	if server.instanceManager.broadcastReady.Load() {
		t.Fatal("broadcast should stay disabled before first broadcast subscribe succeeds")
	}

	server.onBroadcastSubscribeSuccess()

	if !server.instanceManager.broadcastReady.Load() {
		t.Fatal("broadcast should become enabled after broadcast subscribe succeeds")
	}
}

func TestBroadcastSubscribeFailureWithdrawsBroadcastReadyImmediately(t *testing.T) {
	server := newResilienceTestServer()
	server.cfg.WebSocket.CrossInstance.BroadcastSubscribeFailThreshold = 3
	server.instanceManager = &InstanceManager{}
	server.instanceManager.broadcastReady.Store(true)

	server.onBroadcastSubscribeFailure(errors.New("temporary broadcast error"))

	if server.instanceManager.broadcastReady.Load() {
		t.Fatal("broadcast subscribe failure should withdraw broadcast readiness immediately")
	}
	if server.crossCaps.BroadcastSubscribe.Load() != int32(CapabilityReady) {
		t.Fatal("broadcast capability should remain ready before threshold is reached")
	}
}

func TestRouteSubscribeSuccessWaitsForMirrorSyncBeforeReady(t *testing.T) {
	server := newResilienceTestServer()
	server.waitRouteSubscribeReady = true
	server.gate = NewLifecycleGateWithState(false, false)

	attempts := 0
	server.instanceManager = &InstanceManager{
		gate:        server.gate,
		connCountFn: func() int64 { return 0 },
		syncHook: func(context.Context, instanceStatePayload) error {
			attempts++
			if attempts == 1 {
				return errors.New("mirror sync failed")
			}
			return nil
		},
	}

	server.onRouteSubscribeSuccess()

	if server.IsReady() {
		t.Fatal("route subscribe success should stay unready until mirror sync succeeds")
	}
	if !server.hasPendingProjectedState() {
		t.Fatal("route subscribe success should schedule projected mirror retry")
	}

	server.retryPendingInstanceStateSync(context.Background())

	if !server.IsReady() {
		t.Fatal("route subscribe success should become ready after mirror sync retry succeeds")
	}
	if !server.gate.IsRouteable() {
		t.Fatal("route subscribe success should become routeable after mirror sync retry succeeds")
	}
}

func TestBroadcastSubscribeFailureRetriesMirrorSync(t *testing.T) {
	server := newResilienceTestServer()

	attempts := 0
	server.instanceManager = &InstanceManager{
		gate:        server.gate,
		connCountFn: func() int64 { return 0 },
		syncHook: func(context.Context, instanceStatePayload) error {
			attempts++
			if attempts == 1 {
				return errors.New("mirror sync failed")
			}
			return nil
		},
	}
	server.instanceManager.broadcastReady.Store(true)

	server.onBroadcastSubscribeFailure(errors.New("temporary broadcast error"))

	if !server.pendingStateSync.Load() {
		t.Fatal("broadcast withdraw should keep pending mirror sync after sync failure")
	}

	server.retryPendingInstanceStateSync(context.Background())

	if server.pendingStateSync.Load() {
		t.Fatal("broadcast withdraw retry should clear pending mirror sync after success")
	}
	if server.instanceManager.broadcastReady.Load() {
		t.Fatal("broadcast withdraw retry should keep local broadcast readiness disabled")
	}
}

func TestStaleRouteSubscribeSuccessDoesNotReopenAfterFailure(t *testing.T) {
	server := newResilienceTestServer()
	server.waitRouteSubscribeReady = true
	server.gate = NewLifecycleGateWithState(false, false)

	releaseSync := make(chan struct{})
	done := make(chan struct{})
	server.instanceManager = &InstanceManager{
		gate:        server.gate,
		connCountFn: func() int64 { return 0 },
		syncHook: func(context.Context, instanceStatePayload) error {
			<-releaseSync
			return nil
		},
	}

	go func() {
		server.onRouteSubscribeSuccess()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	server.onRouteSubscribeFailure(errors.New("subscribe closed during startup"))
	close(releaseSync)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stale route success did not finish")
	}

	if server.IsReady() {
		t.Fatal("stale route success should not reopen readiness after later failure")
	}
	if server.gate.IsRouteable() {
		t.Fatal("stale route success should not reopen routeable after later failure")
	}
}

func TestBroadcastFailureWinsOverBlockedSuccessSync(t *testing.T) {
	server := newResilienceTestServer()

	releaseSync := make(chan struct{})
	successDone := make(chan struct{})
	failureDone := make(chan struct{})
	var mu sync.Mutex
	payloads := make([]instanceStatePayload, 0, 2)
	server.instanceManager = &InstanceManager{
		gate:        server.gate,
		connCountFn: func() int64 { return 0 },
		syncHook: func(_ context.Context, payload instanceStatePayload) error {
			mu.Lock()
			payloads = append(payloads, payload)
			mu.Unlock()
			<-releaseSync
			return nil
		},
	}
	server.instanceManager.broadcastReady.Store(false)

	go func() {
		server.onBroadcastSubscribeSuccess()
		close(successDone)
	}()

	time.Sleep(50 * time.Millisecond)
	go func() {
		server.onBroadcastSubscribeFailure(errors.New("broadcast subscription closed"))
		close(failureDone)
	}()
	close(releaseSync)

	select {
	case <-successDone:
	case <-time.After(time.Second):
		t.Fatal("blocked broadcast success did not finish")
	}
	select {
	case <-failureDone:
	case <-time.After(time.Second):
		t.Fatal("blocked broadcast failure did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(payloads) < 2 {
		t.Fatalf("expected at least 2 mirror writes, got %d", len(payloads))
	}
	if payloads[len(payloads)-1].BroadcastReady {
		t.Fatal("final broadcast mirror write should keep broadcast_ready=false after failure")
	}
	if server.instanceManager.broadcastReady.Load() {
		t.Fatal("broadcast failure should win over blocked success sync")
	}
}

func newResilienceTestServer() *WsServer {
	cfg := &config.Config{
		WebSocket: config.WebSocketConfig{
			PushChannelSize:      1,
			RemoteEnvChannelSize: 1,
			RouteWriteQueueSize:  1,
			RouteWriteWorkerNum:  1,
			PushWorkerNum:        1,
			CrossInstance: config.CrossInstanceConfig{
				Enabled:                     true,
				RouteSubscribeFailThreshold: 1,
				RecoverProbeIntervalSeconds: 1,
				DrainTimeoutSeconds:         1,
				DrainRouteableGraceSeconds:  1,
				DrainKickBatchSize:          1,
				DrainKickIntervalMS:         1,
			},
			Hybrid: config.HybridConfig{
				Enabled: true,
			},
		},
	}

	server := &WsServer{
		cfg:                  cfg,
		userMap:              NewUserMap(),
		registerChan:         make(chan *Client, 1),
		unregisterChan:       make(chan *Client, 1),
		pushChan:             make(chan *PushTask, 1),
		remoteEnvChan:        make(chan *PushEnvelope, 1),
		routeWriteChan:       make(chan routeWriteEvent, 1),
		routeRepairWake:      make(chan struct{}, 1),
		routeRecoverWake:     make(chan struct{}, 1),
		broadcastRecoverWake: make(chan struct{}, 1),
		gate:                 NewLifecycleGate(),
		crossCaps:            NewCrossInstanceCapabilities(true),
		drainRequests:        make(chan DrainReason, 1),
		instanceId:           "inst-a",
	}
	server.crossCaps.RouteSubscribe.Store(int32(CapabilityReady))
	return server
}

type recordingClientConn struct {
	mu             sync.Mutex
	firstControlAt time.Time
}

func (c *recordingClientConn) ReadMessage() ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (c *recordingClientConn) WriteMessage(data []byte) error {
	return nil
}

func (c *recordingClientConn) WriteControlMessage(data []byte, timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.firstControlAt.IsZero() {
		c.firstControlAt = time.Now()
	}
	return nil
}

func (c *recordingClientConn) Close() error {
	return nil
}

func (c *recordingClientConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *recordingClientConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (c *recordingClientConn) FirstControlAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.firstControlAt
}
