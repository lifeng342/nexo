package handler

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/mbeoliero/nexo/internal/middleware"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/mbeoliero/nexo/pkg/response"
)

type MessageService interface {
	SendMessage(ctx context.Context, senderId string, req *service.SendMessageRequest) (*entity.Message, error)
	PullMessages(ctx context.Context, userId string, req *service.PullMessagesRequest) ([]*entity.Message, int64, error)
	GetMaxSeq(ctx context.Context, userId, conversationId string) (int64, error)
}

type SendGate interface {
	AcquireSendLease() (func(), error)
}

// MessageHandler handles message-related requests
type MessageHandler struct {
	msgService MessageService
	gate       SendGate
}

// NewMessageHandler creates a new MessageHandler
func NewMessageHandler(msgService MessageService, gate SendGate) *MessageHandler {
	return &MessageHandler{msgService: msgService, gate: gate}
}

// SendMessage handles send message request (HTTP fallback)
func (h *MessageHandler) SendMessage(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserId(c)
	if userId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrUnauthorized)
		return
	}

	var req service.SendMessageRequest
	if err := c.BindAndValidate(&req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	if h.gate == nil {
		response.ErrorWithCode(ctx, c, errcode.ErrServerShuttingDown)
		return
	}
	release, err := h.gate.AcquireSendLease()
	if err != nil {
		response.Error(ctx, c, err)
		return
	}
	defer release()

	msg, err := h.msgService.SendMessage(ctx, userId, &req)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, msg.ToMessageInfo())
}

// PullMessages handles pull messages request
func (h *MessageHandler) PullMessages(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserId(c)
	if userId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrUnauthorized)
		return
	}

	conversationId := c.Query("conversation_id")
	if conversationId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	beginSeq, _ := strconv.ParseInt(c.Query("begin_seq"), 10, 64)
	endSeq, _ := strconv.ParseInt(c.Query("end_seq"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))

	req := &service.PullMessagesRequest{
		ConversationId: conversationId,
		BeginSeq:       beginSeq,
		EndSeq:         endSeq,
		Limit:          limit,
	}

	messages, maxSeq, err := h.msgService.PullMessages(ctx, userId, req)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	msgInfos := make([]*any, 0, len(messages))
	for _, msg := range messages {
		info := msg.ToMessageInfo()
		msgInfos = append(msgInfos, func() *any { var i any = info; return &i }())
	}

	response.Success(ctx, c, map[string]any{
		"messages": msgInfos,
		"max_seq":  maxSeq,
	})
}

// GetMaxSeqRequest represents get max seq request
type GetMaxSeqRequest struct {
	ConversationId string `json:"conversation_id"`
}

// GetMaxSeq handles get max seq request
func (h *MessageHandler) GetMaxSeq(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserId(c)
	if userId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrUnauthorized)
		return
	}

	conversationId := c.Query("conversation_id")
	if conversationId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	maxSeq, err := h.msgService.GetMaxSeq(ctx, userId, conversationId)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, map[string]any{
		"max_seq": maxSeq,
	})
}
