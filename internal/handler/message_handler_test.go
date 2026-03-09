package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/mbeoliero/nexo/internal/middleware"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/mbeoliero/nexo/pkg/response"
	"github.com/stretchr/testify/require"
)

type stubSendGate struct {
	acquireErr  error
	acquireCnt  int
	releaseCnt  int
}

func (g *stubSendGate) AcquireSendLease() (func(), error) {
	g.acquireCnt++
	if g.acquireErr != nil {
		return nil, g.acquireErr
	}
	return func() { g.releaseCnt++ }, nil
}

type stubMessageService struct {
	called bool
}

func (s *stubMessageService) SendMessage(ctx context.Context, userID string, req *service.SendMessageRequest) (*entity.Message, error) {
	s.called = true
	return &entity.Message{Id: 1, ConversationId: "c1", Seq: 1, ClientMsgId: req.ClientMsgId, SendAt: 1}, nil
}

func (s *stubMessageService) PullMessages(ctx context.Context, userId string, req *service.PullMessagesRequest) ([]*entity.Message, int64, error) {
	return nil, 0, nil
}

func (s *stubMessageService) GetMaxSeq(ctx context.Context, userId, conversationId string) (int64, error) {
	return 0, nil
}

func TestSendMessageRejectsWhenGateClosed(t *testing.T) {
	gate := &stubSendGate{acquireErr: errcode.ErrServerShuttingDown}
	handler := &MessageHandler{msgService: &stubMessageService{}, gate: gate}
	ctx := context.Background()
	c := app.NewContext(0)
	c.Set(middleware.UserIdKey, "u1")
	c.Request.SetBody([]byte(`{"client_msg_id":"m1","recv_id":"u2","session_type":1,"msg_type":1,"content":{}}`))
	c.Request.Header.SetMethod("POST")
	c.Request.Header.SetContentTypeBytes([]byte("application/json"))

	handler.SendMessage(ctx, c)

	require.Equal(t, 503, c.Response.StatusCode())
	var body response.Response
	err := json.Unmarshal(c.Response.Body(), &body)
	require.NoError(t, err)
	require.Equal(t, errcode.ErrServerShuttingDown.Code, body.Code)
}

func TestSendMessageReleasesLeaseAfterServiceCall(t *testing.T) {
	gate := &stubSendGate{}
	svc := &stubMessageService{}
	handler := NewMessageHandler(svc, gate)
	ctx := context.Background()
	c := app.NewContext(0)
	c.Set(middleware.UserIdKey, "u1")
	c.Request.SetBody([]byte(`{"client_msg_id":"m1","recv_id":"u2","session_type":1,"msg_type":1,"content":{}}`))
	c.Request.Header.SetMethod("POST")
	c.Request.Header.SetContentTypeBytes([]byte("application/json"))

	handler.SendMessage(ctx, c)

	require.Equal(t, 1, gate.acquireCnt)
	require.Equal(t, 1, gate.releaseCnt)
}
