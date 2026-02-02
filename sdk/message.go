package sdk

import (
	"context"
	"strconv"
)

// SendMessage sends a message (single or group chat based on request)
func (c *Client) SendMessage(ctx context.Context, req *SendMessageRequest) (*MessageInfo, error) {
	var result MessageInfo
	if err := c.post(ctx, "/msg/send", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SendTextMessage is a convenience method to send a text message to a single user
func (c *Client) SendTextMessage(ctx context.Context, clientMsgId, recvId, text string) (*MessageInfo, error) {
	return c.SendMessage(ctx, &SendMessageRequest{
		ClientMsgId: clientMsgId,
		RecvId:      recvId,
		SessionType: SessionTypeSingle,
		MsgType:     MsgTypeText,
		Content: MessageContent{
			Text: text,
		},
	})
}

// SendGroupTextMessage is a convenience method to send a text message to a group
func (c *Client) SendGroupTextMessage(ctx context.Context, clientMsgId, groupId, text string) (*MessageInfo, error) {
	return c.SendMessage(ctx, &SendMessageRequest{
		ClientMsgId: clientMsgId,
		GroupId:     groupId,
		SessionType: SessionTypeGroup,
		MsgType:     MsgTypeText,
		Content: MessageContent{
			Text: text,
		},
	})
}

// PullMessages pulls messages from a conversation
func (c *Client) PullMessages(ctx context.Context, conversationId string, beginSeq, endSeq int64, limit int) (*PullMessagesResponse, error) {
	params := map[string]string{
		"conversation_id": conversationId,
	}
	if beginSeq > 0 {
		params["begin_seq"] = strconv.FormatInt(beginSeq, 10)
	}
	if endSeq > 0 {
		params["end_seq"] = strconv.FormatInt(endSeq, 10)
	}
	if limit > 0 {
		params["limit"] = strconv.Itoa(limit)
	}

	var result PullMessagesResponse
	if err := c.get(ctx, "/msg/pull", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetMaxSeq gets the max seq for a conversation
func (c *Client) GetMaxSeq(ctx context.Context, conversationId string) (int64, error) {
	params := map[string]string{"conversation_id": conversationId}
	var result MaxSeqResponse
	if err := c.get(ctx, "/msg/max_seq", params, &result); err != nil {
		return 0, err
	}
	return result.MaxSeq, nil
}
