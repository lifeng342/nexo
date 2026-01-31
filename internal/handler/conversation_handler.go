package handler

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/mbeoliero/nexo/internal/middleware"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/mbeoliero/nexo/pkg/response"
)

// ConversationHandler handles conversation-related requests
type ConversationHandler struct {
	convService *service.ConversationService
}

// NewConversationHandler creates a new ConversationHandler
func NewConversationHandler(convService *service.ConversationService) *ConversationHandler {
	return &ConversationHandler{convService: convService}
}

// GetConversationList handles get conversation list request
func (h *ConversationHandler) GetConversationList(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserId(c)
	if userId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrUnauthorized)
		return
	}

	convs, err := h.convService.GetUserConversations(ctx, userId)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, convs)
}

// GetConversation handles get single conversation request
func (h *ConversationHandler) GetConversation(ctx context.Context, c *app.RequestContext) {
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

	conv, err := h.convService.GetConversation(ctx, userId, conversationId)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, conv)
}

// UpdateConversation handles update conversation settings request
func (h *ConversationHandler) UpdateConversation(ctx context.Context, c *app.RequestContext) {
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

	var req service.UpdateConversationRequest
	if err := c.BindAndValidate(&req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	if err := h.convService.UpdateConversation(ctx, userId, conversationId, &req); err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, nil)
}

// MarkReadRequest represents mark read request
type MarkReadRequest struct {
	ConversationId string `json:"conversation_id"`
	ReadSeq        int64  `json:"read_seq"`
}

// MarkRead handles mark conversation as read request
func (h *ConversationHandler) MarkRead(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserId(c)
	if userId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrUnauthorized)
		return
	}

	var req MarkReadRequest
	if err := c.BindAndValidate(&req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	if err := h.convService.MarkRead(ctx, userId, req.ConversationId, req.ReadSeq); err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, nil)
}

// GetMaxReadSeq handles get max and read seq for a conversation
func (h *ConversationHandler) GetMaxReadSeq(ctx context.Context, c *app.RequestContext) {
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

	maxSeq, readSeq, err := h.convService.GetMaxReadSeq(ctx, userId, conversationId)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	unreadCount := maxSeq - readSeq
	if unreadCount < 0 {
		unreadCount = 0
	}

	response.Success(ctx, c, map[string]interface{}{
		"max_seq":      maxSeq,
		"read_seq":     readSeq,
		"unread_count": unreadCount,
	})
}

// GetUnreadCount handles get unread count request
func (h *ConversationHandler) GetUnreadCount(ctx context.Context, c *app.RequestContext) {
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

	readSeqStr := c.Query("read_seq")
	readSeq, _ := strconv.ParseInt(readSeqStr, 10, 64)

	maxSeq, currentReadSeq, err := h.convService.GetMaxReadSeq(ctx, userId, conversationId)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	// Use provided read_seq or current read_seq
	if readSeq == 0 {
		readSeq = currentReadSeq
	}

	unreadCount := maxSeq - readSeq
	if unreadCount < 0 {
		unreadCount = 0
	}

	response.Success(ctx, c, map[string]interface{}{
		"unread_count": unreadCount,
	})
}
