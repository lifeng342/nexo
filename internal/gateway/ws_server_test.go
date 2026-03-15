package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/stretchr/testify/require"
)

func TestHandleConnectionRejectsWhenIngressClosed(t *testing.T) {
	gate := NewLifecycleGate()
	gate.CloseIngress()
	server := &WsServer{cfg: &config.Config{WebSocket: config.WebSocketConfig{MaxConnNum: 10}}, maxConnNum: 10, gate: gate}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ws?token=t&send_id=u1", nil)

	server.HandleConnection(context.Background(), w, r)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestAsyncPushToUsersRejectsWhenSendPathClosed(t *testing.T) {
	gate := NewLifecycleGate()
	gate.BeginSendDrain()
	gate.CloseSendPath()
	server := &WsServer{pushChan: make(chan *PushTask, 1), gate: gate}

	server.AsyncPushToUsers(nil, []string{"u1"}, "")

	require.Len(t, server.pushChan, 0)
}

func TestHandleSendMsgRejectsWhenSendLeaseUnavailable(t *testing.T) {
	gate := NewLifecycleGate()
	gate.BeginSendDrain()
	server := &WsServer{gate: gate}
	client := &Client{UserId: "u1"}
	req := &WSRequest{Data: []byte(`{"client_msg_id":"m1","recv_id":"u2","session_type":1,"msg_type":1,"content":{}}`)}

	_, err := server.HandleSendMsg(context.Background(), client, req)

	require.ErrorIs(t, err, errcode.ErrServerShuttingDown)
}

func TestUnregisterClientDoesNotDecrementBelowZeroWhenClientMissing(t *testing.T) {
	server := &WsServer{userMap: NewUserMap(nil)}
	client := &Client{UserId: "u1", ConnId: "c1"}

	server.unregisterClient(context.Background(), client)

	require.Equal(t, int64(0), server.onlineConnNum.Load())
	require.Equal(t, int64(0), server.onlineUserNum.Load())
}

func TestRegisterClientQueueFullMarksPendingRegister(t *testing.T) {
	server := newRouteRuntimeTestServer()
	server.routeWriteChan = make(chan routeWriteEvent)
	client := &Client{UserId: "u1", ConnId: "c1", PlatformId: 1}

	server.registerClient(context.Background(), client)

	require.Equal(t, int64(1), server.GetOnlineConnCount())
	state, ok := server.getRouteConnState(routeConnKey{UserID: "u1", ConnID: "c1"})
	require.True(t, ok)
	require.Equal(t, routeDesiredRegistered, state.Desired)
}

func TestUnregisterClientQueueFullDoesNotBlockMainPath(t *testing.T) {
	server := newRouteRuntimeTestServer()
	server.routeWriteChan = make(chan routeWriteEvent)
	client := &Client{UserId: "u1", ConnId: "c1", PlatformId: 1}
	server.userMap.Register(context.Background(), client)
	server.onlineUserNum.Store(1)
	server.onlineConnNum.Store(1)
	server.markRouteConnDesired(routeConnDescriptorFromClient(client, "inst-a"), routeDesiredRegistered)

	server.unregisterClient(context.Background(), client)

	require.Equal(t, int64(0), server.GetOnlineConnCount())
	_, exists := server.userMap.GetAll("u1")
	require.False(t, exists)
	state, ok := server.getRouteConnState(routeConnKey{UserID: "u1", ConnID: "c1"})
	require.True(t, ok)
	require.Equal(t, routeDesiredUnregistered, state.Desired)
}

func TestRouteRepairRetainsPendingRegisterWhenDisconnected(t *testing.T) {
	server := newRouteRuntimeTestServer()
	server.markRouteConnDesired(routeConnDescriptor{UserID: "u1", ConnID: "c1", PlatformID: 1, InstanceID: "inst-a"}, routeDesiredRegistered)

	server.runRouteRepairOnce(context.Background())

	state, ok := server.getRouteConnState(routeConnKey{UserID: "u1", ConnID: "c1"})
	require.True(t, ok)
	require.Equal(t, routeDesiredRegistered, state.Desired)
}

func TestRouteWriteWorkerAppliesLastStateWins(t *testing.T) {
	server := newRouteRuntimeTestServer()
	desc := routeConnDescriptor{UserID: "u1", ConnID: "c1", PlatformID: 1, InstanceID: "inst-a"}
	server.markRouteConnDesired(desc, routeDesiredRegistered)
	server.markRouteConnDesired(desc, routeDesiredUnregistered)

	server.handleRouteWriteEvent(context.Background(), routeWriteEvent{Descriptor: desc, Desired: routeDesiredRegistered})
	server.handleRouteWriteEvent(context.Background(), routeWriteEvent{Descriptor: desc, Desired: routeDesiredUnregistered})

	require.Equal(t, []string{"unregister:u1:c1"}, server.routeRecorder.events)
	_, ok := server.getRouteConnState(routeConnKey{UserID: "u1", ConnID: "c1"})
	require.False(t, ok)
}

func TestTryEnqueueRegisterReturnsErrorWhenContextCancelled(t *testing.T) {
	server := newRouteRuntimeTestServer()
	server.registerChan = make(chan *Client)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := server.tryEnqueueRegister(ctx, &Client{UserId: "u1", ConnId: "c1"})

	require.Error(t, err)
}

func TestHandleRouteWriteEventKeepsRegisteredStateWithoutRouteStore(t *testing.T) {
	server := newRouteRuntimeTestServer()
	client := &Client{UserId: "u1", ConnId: "c1", PlatformId: 1}
	server.userMap.Register(context.Background(), client)
	desc := routeConnDescriptor{UserID: "u1", ConnID: "c1", PlatformID: 1, InstanceID: "inst-a"}
	server.markRouteConnDesired(desc, routeDesiredRegistered)

	server.handleRouteWriteEvent(context.Background(), routeWriteEvent{Descriptor: desc, Desired: routeDesiredRegistered})

	state, ok := server.getRouteConnState(routeConnKey{UserID: "u1", ConnID: "c1"})
	require.True(t, ok)
	require.Equal(t, routeDesiredRegistered, state.Desired)
}

func TestResolvePlatformIDAllowsMatchingPlatform(t *testing.T) {
	resolved, err := resolvePlatformID(5, "5")
	require.NoError(t, err)
	require.Equal(t, 5, resolved)
}

func TestResolvePlatformIDRejectsMismatchedPlatform(t *testing.T) {
	_, err := resolvePlatformID(1, "5")
	require.Error(t, err)
}

func TestResolvePlatformIDRejectsInvalidOverride(t *testing.T) {
	_, err := resolvePlatformID(1, "bad")
	require.Error(t, err)
}

func newRouteRuntimeTestServer() *WsServer {
	cfg := &config.Config{}
	cfg.WebSocket.CrossInstance.Enabled = true
	cfg.WebSocket.CrossInstance.RouteWriteQueueSize = 1
	cfg.WebSocket.CrossInstance.RouteWriteWorkerNum = 1
	cfg.WebSocket.CrossInstance.RouteReconcileIntervalSeconds = 1
	cfg.WebSocket.CrossInstance.RouteStaleCleanupLimit = 100
	server := NewWsServer(cfg, nil, nil, nil, nil)
	server.AttachRouteStore(nil, "inst-a")
	server.routeRecorder = &routeRuntimeRecorder{}
	server.initRouteRuntime()
	return server
}
