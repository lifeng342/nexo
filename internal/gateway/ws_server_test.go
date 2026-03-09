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
