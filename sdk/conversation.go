package sdk

import (
	"context"
	"strconv"
)

// GetConversationList gets all conversations for the current user
func (c *Client) GetConversationList(ctx context.Context) ([]*ConversationInfo, error) {
	var result []*ConversationInfo
	if err := c.get(ctx, "/conversation/list", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetConversation gets a specific conversation
func (c *Client) GetConversation(ctx context.Context, conversationId string) (*ConversationInfo, error) {
	params := map[string]string{"conversation_id": conversationId}
	var result ConversationInfo
	if err := c.get(ctx, "/conversation/info", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateConversation updates conversation settings
func (c *Client) UpdateConversation(ctx context.Context, conversationId string, req *UpdateConversationRequest) error {
	params := map[string]string{"conversation_id": conversationId}
	// Build URL with query parameters for PUT request
	path := "/conversation/update?conversation_id=" + conversationId
	_ = params // params not used in PUT body approach
	return c.put(ctx, path, req, nil)
}

// SetConversationPinned sets the pinned status of a conversation
func (c *Client) SetConversationPinned(ctx context.Context, conversationId string, isPinned bool) error {
	return c.UpdateConversation(ctx, conversationId, &UpdateConversationRequest{
		IsPinned: &isPinned,
	})
}

// SetConversationRecvMsgOpt sets the receive message option of a conversation
func (c *Client) SetConversationRecvMsgOpt(ctx context.Context, conversationId string, recvMsgOpt int32) error {
	return c.UpdateConversation(ctx, conversationId, &UpdateConversationRequest{
		RecvMsgOpt: &recvMsgOpt,
	})
}

// MarkRead marks a conversation as read up to a seq
func (c *Client) MarkRead(ctx context.Context, conversationId string, readSeq int64) error {
	req := &MarkReadRequest{
		ConversationId: conversationId,
		ReadSeq:        readSeq,
	}
	return c.post(ctx, "/conversation/mark_read", req, nil)
}

// GetMaxReadSeq gets the max seq and read seq for a conversation
func (c *Client) GetMaxReadSeq(ctx context.Context, conversationId string) (*MaxReadSeqResponse, error) {
	params := map[string]string{"conversation_id": conversationId}
	var result MaxReadSeqResponse
	if err := c.get(ctx, "/conversation/max_read_seq", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetUnreadCount gets the unread count for a conversation
func (c *Client) GetUnreadCount(ctx context.Context, conversationId string, readSeq int64) (int64, error) {
	params := map[string]string{"conversation_id": conversationId}
	if readSeq > 0 {
		params["read_seq"] = strconv.FormatInt(readSeq, 10)
	}
	var result UnreadCountResponse
	if err := c.get(ctx, "/conversation/unread_count", params, &result); err != nil {
		return 0, err
	}
	return result.UnreadCount, nil
}
