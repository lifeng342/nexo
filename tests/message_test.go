package tests

import (
	"fmt"
	"testing"
)

// MessageContent represents message content
type MessageContent struct {
	Text   string `json:"text,omitempty"`
	Image  string `json:"image,omitempty"`
	Video  string `json:"video,omitempty"`
	Audio  string `json:"audio,omitempty"`
	File   string `json:"file,omitempty"`
	Custom string `json:"custom,omitempty"`
}

// SendMessageRequest represents send message request
type SendMessageRequest struct {
	ClientMsgId string         `json:"client_msg_id"`
	RecvId      string         `json:"recv_id,omitempty"`
	GroupId     string         `json:"group_id,omitempty"`
	SessionType int32          `json:"session_type"`
	MsgType     int32          `json:"msg_type"`
	Content     MessageContent `json:"content"`
}

// MessageInfo represents message info
type MessageInfo struct {
	Id             int64          `json:"id"`
	ConversationId string         `json:"conversation_id"`
	Seq            int64          `json:"seq"`
	ClientMsgId    string         `json:"client_msg_id"`
	SenderId       string         `json:"sender_id"`
	SessionType    int32          `json:"session_type"`
	MsgType        int32          `json:"msg_type"`
	Content        MessageContent `json:"content"`
	SendAt         int64          `json:"send_at"`
}

// PullMessagesResponse represents pull messages response
type PullMessagesResponse struct {
	Messages []MessageInfo `json:"messages"`
	MaxSeq   int64         `json:"max_seq"`
}

// Constants
const (
	SessionTypeSingle = 1
	SessionTypeGroup  = 2
	MsgTypeText       = 1
)

func TestMessage_SendSingleChat(t *testing.T) {
	// Create two users
	user1Id := generateUserId("msg_user1")
	user2Id := generateUserId("msg_user2")
	client1, _ := RegisterAndLogin(t, user1Id, "Message User 1", "password123")
	RegisterAndLogin(t, user2Id, "Message User 2", "password123")

	t.Run("send text message", func(t *testing.T) {
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			RecvId:      user2Id,
			SessionType: SessionTypeSingle,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: "Hello, this is a test message!",
			},
		}

		resp, err := client1.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}

		AssertSuccess(t, resp, "send message should succeed")

		var msgInfo MessageInfo
		if err := resp.ParseData(&msgInfo); err != nil {
			t.Fatalf("parse message info failed: %v", err)
		}

		if msgInfo.Seq <= 0 {
			t.Error("seq should be positive")
		}
		if msgInfo.SenderId != user1Id {
			t.Errorf("expected sender_id=%s, got %s", user1Id, msgInfo.SenderId)
		}
		if msgInfo.Content.Text != "Hello, this is a test message!" {
			t.Errorf("content mismatch")
		}
	})

	t.Run("send message without client_msg_id", func(t *testing.T) {
		req := SendMessageRequest{
			ClientMsgId: "",
			RecvId:      user2Id,
			SessionType: SessionTypeSingle,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: "Message without client_msg_id",
			},
		}

		resp, err := client1.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}

		AssertError(t, resp, 1001, "should return invalid param error")
	})

	t.Run("send message idempotency", func(t *testing.T) {
		clientMsgId := generateClientMsgId()
		req := SendMessageRequest{
			ClientMsgId: clientMsgId,
			RecvId:      user2Id,
			SessionType: SessionTypeSingle,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: "Idempotent message",
			},
		}

		// Send first time
		resp1, err := client1.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}
		AssertSuccess(t, resp1, "first send should succeed")

		var msg1 MessageInfo
		if err := resp1.ParseData(&msg1); err != nil {
			t.Fatalf("parse message info failed: %v", err)
		}

		// Send second time with same client_msg_id
		resp2, err := client1.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}
		AssertSuccess(t, resp2, "second send should also succeed (idempotent)")

		var msg2 MessageInfo
		if err := resp2.ParseData(&msg2); err != nil {
			t.Fatalf("parse message info failed: %v", err)
		}

		// Should return the same message
		if msg1.Seq != msg2.Seq {
			t.Errorf("idempotent sends should return same seq: %d vs %d", msg1.Seq, msg2.Seq)
		}
	})
}

func TestMessage_SendGroupChat(t *testing.T) {
	// Create users
	ownerId := generateUserId("group_msg_owner")
	memberId := generateUserId("group_msg_member")
	ownerClient, _ := RegisterAndLogin(t, ownerId, "Group Msg Owner", "password123")
	memberClient, _ := RegisterAndLogin(t, memberId, "Group Msg Member", "password123")

	// Create group with member
	groupId := CreateGroupAndGetId(t, ownerClient, "Message Test Group", []string{memberId})

	t.Run("owner send group message", func(t *testing.T) {
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			GroupId:     groupId,
			SessionType: SessionTypeGroup,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: "Hello group!",
			},
		}

		resp, err := ownerClient.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}

		AssertSuccess(t, resp, "send group message should succeed")

		var msgInfo MessageInfo
		if err := resp.ParseData(&msgInfo); err != nil {
			t.Fatalf("parse message info failed: %v", err)
		}

		if msgInfo.SenderId != ownerId {
			t.Errorf("expected sender_id=%s, got %s", ownerId, msgInfo.SenderId)
		}
	})

	t.Run("member send group message", func(t *testing.T) {
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			GroupId:     groupId,
			SessionType: SessionTypeGroup,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: "Hello from member!",
			},
		}

		resp, err := memberClient.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}

		AssertSuccess(t, resp, "member send group message should succeed")
	})

	t.Run("non-member cannot send group message", func(t *testing.T) {
		nonMemberId := generateUserId("non_member")
		nonMemberClient, _ := RegisterAndLogin(t, nonMemberId, "Non Member", "password123")

		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			GroupId:     groupId,
			SessionType: SessionTypeGroup,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: "Should not be sent",
			},
		}

		resp, err := nonMemberClient.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}

		AssertError(t, resp, 3003, "non-member should not be able to send")
	})
}

func TestMessage_Pull(t *testing.T) {
	// Create two users
	user1Id := generateUserId("pull_user1")
	user2Id := generateUserId("pull_user2")
	client1, _ := RegisterAndLogin(t, user1Id, "Pull User 1", "password123")
	client2, _ := RegisterAndLogin(t, user2Id, "Pull User 2", "password123")

	// Send some messages
	var conversationId string
	for i := 0; i < 5; i++ {
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			RecvId:      user2Id,
			SessionType: SessionTypeSingle,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: fmt.Sprintf("Message %d", i+1),
			},
		}

		resp, err := client1.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}
		AssertSuccess(t, resp, "send message should succeed")

		if i == 0 {
			var msgInfo MessageInfo
			if err := resp.ParseData(&msgInfo); err != nil {
				t.Fatalf("parse message info failed: %v", err)
			}
			conversationId = msgInfo.ConversationId
		}
	}

	t.Run("pull messages as sender", func(t *testing.T) {
		resp, err := client1.GET(fmt.Sprintf("/msg/pull?conversation_id=%s&begin_seq=1&limit=10", conversationId))
		if err != nil {
			t.Fatalf("pull messages failed: %v", err)
		}

		AssertSuccess(t, resp, "pull messages should succeed")

		var pullResp PullMessagesResponse
		if err := resp.ParseData(&pullResp); err != nil {
			t.Fatalf("parse pull response failed: %v", err)
		}

		if len(pullResp.Messages) != 5 {
			t.Errorf("expected 5 messages, got %d", len(pullResp.Messages))
		}
		if pullResp.MaxSeq != 5 {
			t.Errorf("expected max_seq=5, got %d", pullResp.MaxSeq)
		}
	})

	t.Run("pull messages as receiver", func(t *testing.T) {
		resp, err := client2.GET(fmt.Sprintf("/msg/pull?conversation_id=%s&begin_seq=1&limit=10", conversationId))
		if err != nil {
			t.Fatalf("pull messages failed: %v", err)
		}

		AssertSuccess(t, resp, "pull messages should succeed")

		var pullResp PullMessagesResponse
		if err := resp.ParseData(&pullResp); err != nil {
			t.Fatalf("parse pull response failed: %v", err)
		}

		if len(pullResp.Messages) != 5 {
			t.Errorf("expected 5 messages, got %d", len(pullResp.Messages))
		}
	})

	t.Run("pull messages with limit", func(t *testing.T) {
		resp, err := client1.GET(fmt.Sprintf("/msg/pull?conversation_id=%s&begin_seq=1&limit=3", conversationId))
		if err != nil {
			t.Fatalf("pull messages failed: %v", err)
		}

		AssertSuccess(t, resp, "pull messages should succeed")

		var pullResp PullMessagesResponse
		if err := resp.ParseData(&pullResp); err != nil {
			t.Fatalf("parse pull response failed: %v", err)
		}

		if len(pullResp.Messages) != 3 {
			t.Errorf("expected 3 messages, got %d", len(pullResp.Messages))
		}
	})

	t.Run("pull messages with seq range", func(t *testing.T) {
		resp, err := client1.GET(fmt.Sprintf("/msg/pull?conversation_id=%s&begin_seq=2&end_seq=4&limit=10", conversationId))
		if err != nil {
			t.Fatalf("pull messages failed: %v", err)
		}

		AssertSuccess(t, resp, "pull messages should succeed")

		var pullResp PullMessagesResponse
		if err := resp.ParseData(&pullResp); err != nil {
			t.Fatalf("parse pull response failed: %v", err)
		}

		if len(pullResp.Messages) != 3 {
			t.Errorf("expected 3 messages (seq 2,3,4), got %d", len(pullResp.Messages))
		}
	})

	t.Run("unauthorized user cannot pull messages", func(t *testing.T) {
		unauthorizedId := generateUserId("unauthorized")
		unauthorizedClient, _ := RegisterAndLogin(t, unauthorizedId, "Unauthorized", "password123")

		resp, err := unauthorizedClient.GET(fmt.Sprintf("/msg/pull?conversation_id=%s&begin_seq=1&limit=10", conversationId))
		if err != nil {
			t.Fatalf("pull messages failed: %v", err)
		}

		AssertError(t, resp, 1007, "unauthorized user should not be able to pull")
	})
}

func TestMessage_GetMaxSeq(t *testing.T) {
	// Create two users
	user1Id := generateUserId("maxseq_user1")
	user2Id := generateUserId("maxseq_user2")
	client1, _ := RegisterAndLogin(t, user1Id, "MaxSeq User 1", "password123")
	RegisterAndLogin(t, user2Id, "MaxSeq User 2", "password123")

	// Send a message to create conversation
	req := SendMessageRequest{
		ClientMsgId: generateClientMsgId(),
		RecvId:      user2Id,
		SessionType: SessionTypeSingle,
		MsgType:     MsgTypeText,
		Content: MessageContent{
			Text: "Test message",
		},
	}

	resp, err := client1.POST("/msg/send", req)
	if err != nil {
		t.Fatalf("send message failed: %v", err)
	}
	AssertSuccess(t, resp, "send message should succeed")

	var msgInfo MessageInfo
	if err := resp.ParseData(&msgInfo); err != nil {
		t.Fatalf("parse message info failed: %v", err)
	}

	t.Run("get max seq", func(t *testing.T) {
		resp, err := client1.GET(fmt.Sprintf("/msg/max_seq?conversation_id=%s", msgInfo.ConversationId))
		if err != nil {
			t.Fatalf("get max seq failed: %v", err)
		}

		AssertSuccess(t, resp, "get max seq should succeed")

		var result map[string]int64
		if err := resp.ParseData(&result); err != nil {
			t.Fatalf("parse result failed: %v", err)
		}

		if result["max_seq"] != 1 {
			t.Errorf("expected max_seq=1, got %d", result["max_seq"])
		}
	})
}
