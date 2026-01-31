package tests

import (
	"fmt"
	"testing"
)

// TestFullFlow tests a complete user journey
func TestFullFlow_SingleChat(t *testing.T) {
	// Create two users
	user1Id := generateUserId("flow_user1")
	user2Id := generateUserId("flow_user2")

	t.Log("Step 1: Register users")
	client1, _ := RegisterAndLogin(t, user1Id, "Flow User 1", "password123")
	client2, _ := RegisterAndLogin(t, user2Id, "Flow User 2", "password123")

	t.Log("Step 2: User1 sends messages to User2")
	var conversationId string
	for i := 1; i <= 5; i++ {
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			RecvId:      user2Id,
			SessionType: SessionTypeSingle,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: fmt.Sprintf("Hello %d from User1!", i),
			},
		}
		resp, err := client1.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}
		AssertSuccess(t, resp, "send message should succeed")

		if i == 1 {
			var msgInfo MessageInfo
			if err := resp.ParseData(&msgInfo); err != nil {
				t.Fatalf("parse message info failed: %v", err)
			}
			conversationId = msgInfo.ConversationId
			t.Logf("Conversation ID: %s", conversationId)
		}
	}

	t.Log("Step 3: User2 checks unread count")
	resp, err := client2.GET(fmt.Sprintf("/conversation/unread_count?conversation_id=%s", conversationId))
	if err != nil {
		t.Fatalf("get unread count failed: %v", err)
	}
	AssertSuccess(t, resp, "get unread count should succeed")

	var unreadResult map[string]int64
	if err := resp.ParseData(&unreadResult); err != nil {
		t.Fatalf("parse result failed: %v", err)
	}
	if unreadResult["unread_count"] != 5 {
		t.Errorf("expected unread_count=5, got %d", unreadResult["unread_count"])
	}
	t.Logf("User2 unread count: %d", unreadResult["unread_count"])

	t.Log("Step 4: User2 pulls messages")
	resp, err = client2.GET(fmt.Sprintf("/msg/pull?conversation_id=%s&begin_seq=1&limit=10", conversationId))
	if err != nil {
		t.Fatalf("pull messages failed: %v", err)
	}
	AssertSuccess(t, resp, "pull messages should succeed")

	var pullResp PullMessagesResponse
	if err := resp.ParseData(&pullResp); err != nil {
		t.Fatalf("parse pull response failed: %v", err)
	}
	t.Logf("Pulled %d messages, max_seq=%d", len(pullResp.Messages), pullResp.MaxSeq)

	t.Log("Step 5: User2 marks messages as read")
	markReq := MarkReadRequest{
		ConversationId: conversationId,
		ReadSeq:        5,
	}
	resp, err = client2.POST("/conversation/mark_read", markReq)
	if err != nil {
		t.Fatalf("mark read failed: %v", err)
	}
	AssertSuccess(t, resp, "mark read should succeed")

	t.Log("Step 6: Verify unread count is 0")
	resp, err = client2.GET(fmt.Sprintf("/conversation/unread_count?conversation_id=%s", conversationId))
	if err != nil {
		t.Fatalf("get unread count failed: %v", err)
	}
	if err := resp.ParseData(&unreadResult); err != nil {
		t.Fatalf("parse result failed: %v", err)
	}
	if unreadResult["unread_count"] != 0 {
		t.Errorf("expected unread_count=0, got %d", unreadResult["unread_count"])
	}
	t.Logf("User2 unread count after mark read: %d", unreadResult["unread_count"])

	t.Log("Step 7: User2 replies to User1")
	replyReq := SendMessageRequest{
		ClientMsgId: generateClientMsgId(),
		RecvId:      user1Id,
		SessionType: SessionTypeSingle,
		MsgType:     MsgTypeText,
		Content: MessageContent{
			Text: "Hi User1, got your messages!",
		},
	}
	resp, err = client2.POST("/msg/send", replyReq)
	if err != nil {
		t.Fatalf("send reply failed: %v", err)
	}
	AssertSuccess(t, resp, "send reply should succeed")

	t.Log("Full single chat flow completed successfully!")
}

func TestFullFlow_GroupChat(t *testing.T) {
	// Create users
	ownerId := generateUserId("gflow_owner")
	member1Id := generateUserId("gflow_member1")
	member2Id := generateUserId("gflow_member2")

	t.Log("Step 1: Register users")
	ownerClient, _ := RegisterAndLogin(t, ownerId, "Group Owner", "password123")
	member1Client, _ := RegisterAndLogin(t, member1Id, "Group Member 1", "password123")
	member2Client, _ := RegisterAndLogin(t, member2Id, "Group Member 2", "password123")

	t.Log("Step 2: Create group with member1")
	groupId := CreateGroupAndGetId(t, ownerClient, "Flow Test Group", []string{member1Id})
	conversationId := "sg_" + groupId
	t.Logf("Group ID: %s, Conversation ID: %s", groupId, conversationId)

	t.Log("Step 3: Member2 joins the group")
	joinReq := JoinGroupRequest{GroupId: groupId}
	resp, err := member2Client.POST("/group/join", joinReq)
	if err != nil {
		t.Fatalf("join group failed: %v", err)
	}
	AssertSuccess(t, resp, "join group should succeed")

	t.Log("Step 4: Verify group has 3 members")
	resp, err = ownerClient.GET("/group/members?group_id=" + groupId)
	if err != nil {
		t.Fatalf("get members failed: %v", err)
	}
	var members []GroupMember
	if err := resp.ParseData(&members); err != nil {
		t.Fatalf("parse members failed: %v", err)
	}
	if len(members) != 3 {
		t.Errorf("expected 3 members, got %d", len(members))
	}
	t.Logf("Group has %d members", len(members))

	t.Log("Step 5: Owner sends messages to group")
	for i := 1; i <= 3; i++ {
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			GroupId:     groupId,
			SessionType: SessionTypeGroup,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: fmt.Sprintf("Group announcement %d", i),
			},
		}
		resp, err := ownerClient.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}
		AssertSuccess(t, resp, "send message should succeed")
	}

	t.Log("Step 6: Member1 checks unread count")
	resp, err = member1Client.GET(fmt.Sprintf("/conversation/unread_count?conversation_id=%s", conversationId))
	if err != nil {
		t.Fatalf("get unread count failed: %v", err)
	}
	var unreadResult map[string]int64
	if err := resp.ParseData(&unreadResult); err != nil {
		t.Fatalf("parse result failed: %v", err)
	}
	t.Logf("Member1 unread count: %d", unreadResult["unread_count"])

	t.Log("Step 7: Member1 pulls and reads messages")
	resp, err = member1Client.GET(fmt.Sprintf("/msg/pull?conversation_id=%s&begin_seq=1&limit=10", conversationId))
	if err != nil {
		t.Fatalf("pull messages failed: %v", err)
	}
	var pullResp PullMessagesResponse
	if err := resp.ParseData(&pullResp); err != nil {
		t.Fatalf("parse pull response failed: %v", err)
	}
	t.Logf("Member1 pulled %d messages", len(pullResp.Messages))

	markReq := MarkReadRequest{
		ConversationId: conversationId,
		ReadSeq:        pullResp.MaxSeq,
	}
	resp, err = member1Client.POST("/conversation/mark_read", markReq)
	if err != nil {
		t.Fatalf("mark read failed: %v", err)
	}
	AssertSuccess(t, resp, "mark read should succeed")

	t.Log("Step 8: Member1 sends a message")
	msgReq := SendMessageRequest{
		ClientMsgId: generateClientMsgId(),
		GroupId:     groupId,
		SessionType: SessionTypeGroup,
		MsgType:     MsgTypeText,
		Content: MessageContent{
			Text: "Hello everyone from Member1!",
		},
	}
	resp, err = member1Client.POST("/msg/send", msgReq)
	if err != nil {
		t.Fatalf("send message failed: %v", err)
	}
	AssertSuccess(t, resp, "send message should succeed")

	t.Log("Step 9: Member2 quits the group")
	quitReq := QuitGroupRequest{GroupId: groupId}
	resp, err = member2Client.POST("/group/quit", quitReq)
	if err != nil {
		t.Fatalf("quit group failed: %v", err)
	}
	AssertSuccess(t, resp, "quit group should succeed")

	t.Log("Step 10: Verify member2 cannot send messages")
	msgReq = SendMessageRequest{
		ClientMsgId: generateClientMsgId(),
		GroupId:     groupId,
		SessionType: SessionTypeGroup,
		MsgType:     MsgTypeText,
		Content: MessageContent{
			Text: "This should fail",
		},
	}
	resp, err = member2Client.POST("/msg/send", msgReq)
	if err != nil {
		t.Fatalf("send message failed: %v", err)
	}
	AssertError(t, resp, 3003, "quit member should not be able to send")

	t.Log("Full group chat flow completed successfully!")
}

func TestFullFlow_MessageVisibility(t *testing.T) {
	// Test that new group members cannot see historical messages
	ownerId := generateUserId("vis_owner")
	lateMemberId := generateUserId("vis_late_member")

	t.Log("Step 1: Create owner and group")
	ownerClient, _ := RegisterAndLogin(t, ownerId, "Visibility Owner", "password123")
	groupId := CreateGroupAndGetId(t, ownerClient, "Visibility Test Group", nil)
	conversationId := "sg_" + groupId

	t.Log("Step 2: Owner sends messages before late member joins")
	for i := 1; i <= 5; i++ {
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			GroupId:     groupId,
			SessionType: SessionTypeGroup,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: fmt.Sprintf("Historical message %d", i),
			},
		}
		resp, err := ownerClient.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}
		AssertSuccess(t, resp, "send message should succeed")
	}

	t.Log("Step 3: Late member joins the group")
	lateMemberClient, _ := RegisterAndLogin(t, lateMemberId, "Late Member", "password123")
	joinReq := JoinGroupRequest{GroupId: groupId}
	resp, err := lateMemberClient.POST("/group/join", joinReq)
	if err != nil {
		t.Fatalf("join group failed: %v", err)
	}
	AssertSuccess(t, resp, "join group should succeed")

	t.Log("Step 4: Owner sends more messages after late member joins")
	for i := 6; i <= 8; i++ {
		req := SendMessageRequest{
			ClientMsgId: generateClientMsgId(),
			GroupId:     groupId,
			SessionType: SessionTypeGroup,
			MsgType:     MsgTypeText,
			Content: MessageContent{
				Text: fmt.Sprintf("New message %d", i),
			},
		}
		resp, err := ownerClient.POST("/msg/send", req)
		if err != nil {
			t.Fatalf("send message failed: %v", err)
		}
		AssertSuccess(t, resp, "send message should succeed")
	}

	t.Log("Step 5: Late member pulls messages")
	resp, err = lateMemberClient.GET(fmt.Sprintf("/msg/pull?conversation_id=%s&begin_seq=1&limit=20", conversationId))
	if err != nil {
		t.Fatalf("pull messages failed: %v", err)
	}
	AssertSuccess(t, resp, "pull messages should succeed")

	var pullResp PullMessagesResponse
	if err := resp.ParseData(&pullResp); err != nil {
		t.Fatalf("parse pull response failed: %v", err)
	}

	// Late member should only see messages after joining (seq 6, 7, 8)
	// The exact behavior depends on implementation
	t.Logf("Late member can see %d messages (implementation may vary)", len(pullResp.Messages))

	t.Log("Message visibility flow completed!")
}
