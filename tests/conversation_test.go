package tests

import (
	"fmt"
	"testing"
)

// ConversationInfo represents conversation info
type ConversationInfo struct {
	ConversationId   string `json:"conversation_id"`
	OwnerId          string `json:"owner_id"`
	ConversationType int    `json:"conversation_type"`
	PeerUserId       string `json:"peer_user_id,omitempty"`
	GroupId          string `json:"group_id,omitempty"`
	IsPinned         bool   `json:"is_pinned"`
	RecvMsgOpt       int    `json:"recv_msg_opt"`
	MaxSeq           int64  `json:"max_seq"`
	ReadSeq          int64  `json:"read_seq"`
	UnreadCount      int64  `json:"unread_count"`
}

// MarkReadRequest represents mark read request
type MarkReadRequest struct {
	ConversationId string `json:"conversation_id"`
	ReadSeq        int64  `json:"read_seq"`
}

// UpdateConversationRequest represents update conversation request
type UpdateConversationRequest struct {
	IsPinned   *bool `json:"is_pinned,omitempty"`
	RecvMsgOpt *int  `json:"recv_msg_opt,omitempty"`
}

func TestConversation_List(t *testing.T) {
	// Create users
	user1Id := generateUserId("conv_user1")
	user2Id := generateUserId("conv_user2")
	user3Id := generateUserId("conv_user3")
	client1, _ := RegisterAndLogin(t, user1Id, "Conv User 1", "password123")
	RegisterAndLogin(t, user2Id, "Conv User 2", "password123")
	RegisterAndLogin(t, user3Id, "Conv User 3", "password123")

	// Send messages to create conversations
	for _, recvId := range []string{user2Id, user3Id} {
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			RecvId:      recvId,
			SessionType: SessionTypeSingle,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: "Hello!",
			},
		}
		resp, err := client1.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}
		AssertSuccess(t, resp, "send message should succeed")
	}

	t.Run("get conversation list", func(t *testing.T) {
		resp, err := client1.GET("/conversation/list")
		if err != nil {
			t.Fatalf("get conversation list failed: %v", err)
		}

		AssertSuccess(t, resp, "get conversation list should succeed")

		var convs []ConversationInfo
		if err := resp.ParseData(&convs); err != nil {
			t.Fatalf("parse conversations failed: %v", err)
		}

		if len(convs) < 2 {
			t.Errorf("expected at least 2 conversations, got %d", len(convs))
		}
	})
}

func TestConversation_GetInfo(t *testing.T) {
	// Create users
	user1Id := generateUserId("convinfo_user1")
	user2Id := generateUserId("convinfo_user2")
	client1, _ := RegisterAndLogin(t, user1Id, "ConvInfo User 1", "password123")
	RegisterAndLogin(t, user2Id, "ConvInfo User 2", "password123")

	// Send a message to create conversation
	req := SendMessageRequest{
		ClientMsgId: generateClientMsgId(),
		RecvId:      user2Id,
		SessionType: SessionTypeSingle,
		MsgType:     MsgTypeText,
		Content: MessageContent{
			Text: "Hello!",
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

	t.Run("get conversation info", func(t *testing.T) {
		resp, err := client1.GET(fmt.Sprintf("/conversation/info?conversation_id=%s", msgInfo.ConversationId))
		if err != nil {
			t.Fatalf("get conversation info failed: %v", err)
		}

		AssertSuccess(t, resp, "get conversation info should succeed")

		var convInfo ConversationInfo
		if err := resp.ParseData(&convInfo); err != nil {
			t.Fatalf("parse conversation info failed: %v", err)
		}

		if convInfo.ConversationId != msgInfo.ConversationId {
			t.Errorf("conversation_id mismatch")
		}
	})

	t.Run("get conversation info without conversation_id", func(t *testing.T) {
		resp, err := client1.GET("/conversation/info")
		if err != nil {
			t.Fatalf("get conversation info failed: %v", err)
		}

		AssertError(t, resp, 1001, "should return invalid param error")
	})
}

func TestConversation_MarkRead(t *testing.T) {
	// Create users
	user1Id := generateUserId("markread_user1")
	user2Id := generateUserId("markread_user2")
	client1, _ := RegisterAndLogin(t, user1Id, "MarkRead User 1", "password123")
	client2, _ := RegisterAndLogin(t, user2Id, "MarkRead User 2", "password123")

	// Send messages
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

	t.Run("mark read", func(t *testing.T) {
		req := MarkReadRequest{
			ConversationId: conversationId,
			ReadSeq:        3,
		}

		resp, err := client2.POST("/conversation/mark_read", req)
		if err != nil {
			t.Fatalf("mark read failed: %v", err)
		}

		AssertSuccess(t, resp, "mark read should succeed")

		// Verify unread count
		resp, err = client2.GET(fmt.Sprintf("/conversation/unread_count?conversation_id=%s", conversationId))
		if err != nil {
			t.Fatalf("get unread count failed: %v", err)
		}

		AssertSuccess(t, resp, "get unread count should succeed")

		var result map[string]int64
		if err := resp.ParseData(&result); err != nil {
			t.Fatalf("parse result failed: %v", err)
		}

		// max_seq=5, read_seq=3, unread=2
		if result["unread_count"] != 2 {
			t.Errorf("expected unread_count=2, got %d", result["unread_count"])
		}
	})

	t.Run("mark all read", func(t *testing.T) {
		req := MarkReadRequest{
			ConversationId: conversationId,
			ReadSeq:        5,
		}

		resp, err := client2.POST("/conversation/mark_read", req)
		if err != nil {
			t.Fatalf("mark read failed: %v", err)
		}

		AssertSuccess(t, resp, "mark read should succeed")

		// Verify unread count is 0
		resp, err = client2.GET(fmt.Sprintf("/conversation/unread_count?conversation_id=%s", conversationId))
		if err != nil {
			t.Fatalf("get unread count failed: %v", err)
		}

		var result map[string]int64
		if err := resp.ParseData(&result); err != nil {
			t.Fatalf("parse result failed: %v", err)
		}

		if result["unread_count"] != 0 {
			t.Errorf("expected unread_count=0, got %d", result["unread_count"])
		}
	})
}

func TestConversation_GetMaxReadSeq(t *testing.T) {
	// Create users
	user1Id := generateUserId("maxreadseq_user1")
	user2Id := generateUserId("maxreadseq_user2")
	client1, _ := RegisterAndLogin(t, user1Id, "MaxReadSeq User 1", "password123")
	client2, _ := RegisterAndLogin(t, user2Id, "MaxReadSeq User 2", "password123")

	// Send messages
	var conversationId string
	for i := 0; i < 3; i++ {
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

	t.Run("get max read seq", func(t *testing.T) {
		resp, err := client2.GET(fmt.Sprintf("/conversation/max_read_seq?conversation_id=%s", conversationId))
		if err != nil {
			t.Fatalf("get max read seq failed: %v", err)
		}

		AssertSuccess(t, resp, "get max read seq should succeed")

		var result map[string]int64
		if err := resp.ParseData(&result); err != nil {
			t.Fatalf("parse result failed: %v", err)
		}

		if result["max_seq"] != 3 {
			t.Errorf("expected max_seq=3, got %d", result["max_seq"])
		}
	})
}

func TestConversation_UnreadCount(t *testing.T) {
	// Create users
	user1Id := generateUserId("unread_user1")
	user2Id := generateUserId("unread_user2")
	client1, _ := RegisterAndLogin(t, user1Id, "Unread User 1", "password123")
	client2, _ := RegisterAndLogin(t, user2Id, "Unread User 2", "password123")

	// Send messages
	var conversationId string
	for i := 0; i < 10; i++ {
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

	t.Run("get unread count for receiver", func(t *testing.T) {
		resp, err := client2.GET(fmt.Sprintf("/conversation/unread_count?conversation_id=%s", conversationId))
		if err != nil {
			t.Fatalf("get unread count failed: %v", err)
		}

		AssertSuccess(t, resp, "get unread count should succeed")

		var result map[string]int64
		if err := resp.ParseData(&result); err != nil {
			t.Fatalf("parse result failed: %v", err)
		}

		if result["unread_count"] != 10 {
			t.Errorf("expected unread_count=10, got %d", result["unread_count"])
		}
	})

	t.Run("sender has no unread", func(t *testing.T) {
		resp, err := client1.GET(fmt.Sprintf("/conversation/unread_count?conversation_id=%s", conversationId))
		if err != nil {
			t.Fatalf("get unread count failed: %v", err)
		}

		AssertSuccess(t, resp, "get unread count should succeed")

		var result map[string]int64
		if err := resp.ParseData(&result); err != nil {
			t.Fatalf("parse result failed: %v", err)
		}

		// Sender's read_seq is updated when sending, so unread should be 0
		if result["unread_count"] != 0 {
			t.Errorf("expected unread_count=0 for sender, got %d", result["unread_count"])
		}
	})
}

func TestConversation_GroupChat(t *testing.T) {
	// Create users
	ownerId := generateUserId("groupconv_owner")
	memberId := generateUserId("groupconv_member")
	ownerClient, _ := RegisterAndLogin(t, ownerId, "GroupConv Owner", "password123")
	memberClient, _ := RegisterAndLogin(t, memberId, "GroupConv Member", "password123")

	// Create group
	groupId := CreateGroupAndGetId(t, ownerClient, "Conv Test Group", []string{memberId})
	conversationId := "sg_" + groupId

	// Send messages
	for i := 0; i < 5; i++ {
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			GroupId:     groupId,
			SessionType: SessionTypeGroup,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: fmt.Sprintf("Group message %d", i+1),
			},
		}
		resp, err := ownerClient.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}
		AssertSuccess(t, resp, "send message should succeed")
	}

	t.Run("member get unread count", func(t *testing.T) {
		resp, err := memberClient.GET(fmt.Sprintf("/conversation/unread_count?conversation_id=%s", conversationId))
		if err != nil {
			t.Fatalf("get unread count failed: %v", err)
		}

		AssertSuccess(t, resp, "get unread count should succeed")

		var result map[string]int64
		if err := resp.ParseData(&result); err != nil {
			t.Fatalf("parse result failed: %v", err)
		}

		if result["unread_count"] != 5 {
			t.Errorf("expected unread_count=5, got %d", result["unread_count"])
		}
	})

	t.Run("member mark read", func(t *testing.T) {
		req := MarkReadRequest{
			ConversationId: conversationId,
			ReadSeq:        5,
		}

		resp, err := memberClient.POST("/conversation/mark_read", req)
		if err != nil {
			t.Fatalf("mark read failed: %v", err)
		}

		AssertSuccess(t, resp, "mark read should succeed")

		// Verify
		resp, err = memberClient.GET(fmt.Sprintf("/conversation/unread_count?conversation_id=%s", conversationId))
		if err != nil {
			t.Fatalf("get unread count failed: %v", err)
		}

		var result map[string]int64
		if err := resp.ParseData(&result); err != nil {
			t.Fatalf("parse result failed: %v", err)
		}

		if result["unread_count"] != 0 {
			t.Errorf("expected unread_count=0, got %d", result["unread_count"])
		}
	})
}
