package tests

import (
	"testing"
)

// CreateGroupRequest represents group creation request
type CreateGroupRequest struct {
	Name         string   `json:"name"`
	Introduction string   `json:"introduction,omitempty"`
	Avatar       string   `json:"avatar,omitempty"`
	MemberIds    []string `json:"member_ids,omitempty"`
}

// GroupInfo represents group info
type GroupInfo struct {
	Id            string `json:"id"`
	Name          string `json:"name"`
	Introduction  string `json:"introduction"`
	Avatar        string `json:"avatar"`
	Status        int    `json:"status"`
	CreatorUserId string `json:"creator_user_id"`
	MemberCount   int    `json:"member_count"`
	CreatedAt     int64  `json:"created_at"`
}

// JoinGroupRequest represents join group request
type JoinGroupRequest struct {
	GroupId   string `json:"group_id"`
	InviterId string `json:"inviter_id,omitempty"`
}

// QuitGroupRequest represents quit group request
type QuitGroupRequest struct {
	GroupId string `json:"group_id"`
}

// GroupMember represents group member
type GroupMember struct {
	GroupId       string `json:"group_id"`
	UserId        string `json:"user_id"`
	RoleLevel     int    `json:"role_level"`
	Status        int    `json:"status"`
	JoinedAt      int64  `json:"joined_at"`
	InviterUserId string `json:"inviter_user_id"`
}

func TestGroup_Create(t *testing.T) {
	userId := generateUserId("group_creator")
	client, _ := RegisterAndLogin(t, userId, "Group Creator", "password123")

	t.Run("create group", func(t *testing.T) {
		req := CreateGroupRequest{
			Name:         "Test Group",
			Introduction: "This is a test group",
		}

		resp, err := client.POST("/group/create", req)
		if err != nil {
			t.Fatalf("create group failed: %v", err)
		}

		AssertSuccess(t, resp, "create group should succeed")

		var groupInfo GroupInfo
		if err := resp.ParseData(&groupInfo); err != nil {
			t.Fatalf("parse group info failed: %v", err)
		}

		if groupInfo.Id == "" {
			t.Error("group id should not be empty")
		}
		if groupInfo.Name != "Test Group" {
			t.Errorf("expected name=Test Group, got %s", groupInfo.Name)
		}
		if groupInfo.CreatorUserId != userId {
			t.Errorf("expected creator_user_id=%s, got %s", userId, groupInfo.CreatorUserId)
		}
	})

	t.Run("create group with initial members", func(t *testing.T) {
		// Create some users to add as members
		member1Id := generateUserId("member1")
		member2Id := generateUserId("member2")
		RegisterAndLogin(t, member1Id, "Member 1", "password123")
		RegisterAndLogin(t, member2Id, "Member 2", "password123")

		req := CreateGroupRequest{
			Name:      "Group With Members",
			MemberIds: []string{member1Id, member2Id},
		}

		resp, err := client.POST("/group/create", req)
		if err != nil {
			t.Fatalf("create group failed: %v", err)
		}

		AssertSuccess(t, resp, "create group with members should succeed")

		var groupInfo GroupInfo
		if err := resp.ParseData(&groupInfo); err != nil {
			t.Fatalf("parse group info failed: %v", err)
		}

		// Verify members
		resp, err = client.GET("/group/members?group_id=" + groupInfo.Id)
		if err != nil {
			t.Fatalf("get group members failed: %v", err)
		}

		AssertSuccess(t, resp, "get group members should succeed")

		var members []GroupMember
		if err := resp.ParseData(&members); err != nil {
			t.Fatalf("parse members failed: %v", err)
		}

		// Should have 3 members: creator + 2 initial members
		if len(members) != 3 {
			t.Errorf("expected 3 members, got %d", len(members))
		}
	})
}

func TestGroup_JoinAndQuit(t *testing.T) {
	// Create group owner
	ownerId := generateUserId("group_owner")
	ownerClient, _ := RegisterAndLogin(t, ownerId, "Group Owner", "password123")

	// Create a group
	createReq := CreateGroupRequest{
		Name: "Join Test Group",
	}
	resp, err := ownerClient.POST("/group/create", createReq)
	if err != nil {
		t.Fatalf("create group failed: %v", err)
	}
	AssertSuccess(t, resp, "create group should succeed")

	var groupInfo GroupInfo
	if err := resp.ParseData(&groupInfo); err != nil {
		t.Fatalf("parse group info failed: %v", err)
	}
	groupId := groupInfo.Id

	// Create a user to join
	memberId := generateUserId("joiner")
	memberClient, _ := RegisterAndLogin(t, memberId, "Joiner", "password123")

	t.Run("join group", func(t *testing.T) {
		req := JoinGroupRequest{
			GroupId: groupId,
		}

		resp, err := memberClient.POST("/group/join", req)
		if err != nil {
			t.Fatalf("join group failed: %v", err)
		}

		AssertSuccess(t, resp, "join group should succeed")

		// Verify membership
		resp, err = ownerClient.GET("/group/members?group_id=" + groupId)
		if err != nil {
			t.Fatalf("get group members failed: %v", err)
		}

		var members []GroupMember
		if err := resp.ParseData(&members); err != nil {
			t.Fatalf("parse members failed: %v", err)
		}

		found := false
		for _, m := range members {
			if m.UserId == memberId {
				found = true
				break
			}
		}
		if !found {
			t.Error("joined user should be in members list")
		}
	})

	t.Run("join group again (already member)", func(t *testing.T) {
		req := JoinGroupRequest{
			GroupId: groupId,
		}

		resp, err := memberClient.POST("/group/join", req)
		if err != nil {
			t.Fatalf("join group failed: %v", err)
		}

		AssertError(t, resp, 3005, "should return already member error")
	})

	t.Run("quit group", func(t *testing.T) {
		req := QuitGroupRequest{
			GroupId: groupId,
		}

		resp, err := memberClient.POST("/group/quit", req)
		if err != nil {
			t.Fatalf("quit group failed: %v", err)
		}

		AssertSuccess(t, resp, "quit group should succeed")
	})

	t.Run("quit group again (not member)", func(t *testing.T) {
		req := QuitGroupRequest{
			GroupId: groupId,
		}

		resp, err := memberClient.POST("/group/quit", req)
		if err != nil {
			t.Fatalf("quit group failed: %v", err)
		}

		AssertError(t, resp, 3003, "should return not member error")
	})

	t.Run("owner cannot quit group", func(t *testing.T) {
		req := QuitGroupRequest{
			GroupId: groupId,
		}

		resp, err := ownerClient.POST("/group/quit", req)
		if err != nil {
			t.Fatalf("quit group failed: %v", err)
		}

		AssertError(t, resp, 3008, "owner should not be able to quit")
	})
}

func TestGroup_GetInfo(t *testing.T) {
	userId := generateUserId("group_info_user")
	client, _ := RegisterAndLogin(t, userId, "Group Info User", "password123")

	// Create a group
	createReq := CreateGroupRequest{
		Name:         "Info Test Group",
		Introduction: "Test introduction",
	}
	resp, err := client.POST("/group/create", createReq)
	if err != nil {
		t.Fatalf("create group failed: %v", err)
	}
	AssertSuccess(t, resp, "create group should succeed")

	var createdGroup GroupInfo
	if err := resp.ParseData(&createdGroup); err != nil {
		t.Fatalf("parse group info failed: %v", err)
	}

	t.Run("get group info", func(t *testing.T) {
		resp, err := client.GET("/group/info?group_id=" + createdGroup.Id)
		if err != nil {
			t.Fatalf("get group info failed: %v", err)
		}

		AssertSuccess(t, resp, "get group info should succeed")

		var groupInfo GroupInfo
		if err := resp.ParseData(&groupInfo); err != nil {
			t.Fatalf("parse group info failed: %v", err)
		}

		if groupInfo.Id != createdGroup.Id {
			t.Errorf("expected id=%s, got %s", createdGroup.Id, groupInfo.Id)
		}
		if groupInfo.Name != "Info Test Group" {
			t.Errorf("expected name=Info Test Group, got %s", groupInfo.Name)
		}
	})

	t.Run("get non-existent group info", func(t *testing.T) {
		resp, err := client.GET("/group/info?group_id=non_existent_group")
		if err != nil {
			t.Fatalf("get group info failed: %v", err)
		}

		AssertError(t, resp, 3001, "should return group not found error")
	})

	t.Run("get group info without group_id", func(t *testing.T) {
		resp, err := client.GET("/group/info")
		if err != nil {
			t.Fatalf("get group info failed: %v", err)
		}

		AssertError(t, resp, 1001, "should return invalid param error")
	})
}

func TestGroup_GetMembers(t *testing.T) {
	userId := generateUserId("members_user")
	client, _ := RegisterAndLogin(t, userId, "Members User", "password123")

	// Create members
	member1Id := generateUserId("gm_member1")
	member2Id := generateUserId("gm_member2")
	RegisterAndLogin(t, member1Id, "GM Member 1", "password123")
	RegisterAndLogin(t, member2Id, "GM Member 2", "password123")

	// Create a group with members
	createReq := CreateGroupRequest{
		Name:      "Members Test Group",
		MemberIds: []string{member1Id, member2Id},
	}
	resp, err := client.POST("/group/create", createReq)
	if err != nil {
		t.Fatalf("create group failed: %v", err)
	}
	AssertSuccess(t, resp, "create group should succeed")

	var groupInfo GroupInfo
	if err := resp.ParseData(&groupInfo); err != nil {
		t.Fatalf("parse group info failed: %v", err)
	}

	t.Run("get group members", func(t *testing.T) {
		resp, err := client.GET("/group/members?group_id=" + groupInfo.Id)
		if err != nil {
			t.Fatalf("get group members failed: %v", err)
		}

		AssertSuccess(t, resp, "get group members should succeed")

		var members []GroupMember
		if err := resp.ParseData(&members); err != nil {
			t.Fatalf("parse members failed: %v", err)
		}

		if len(members) != 3 {
			t.Errorf("expected 3 members, got %d", len(members))
		}

		// Check owner role
		for _, m := range members {
			if m.UserId == userId {
				if m.RoleLevel != 2 { // Owner
					t.Errorf("creator should have role_level=2, got %d", m.RoleLevel)
				}
			} else {
				if m.RoleLevel != 0 { // Member
					t.Errorf("member should have role_level=0, got %d", m.RoleLevel)
				}
			}
		}
	})
}

// CreateGroupAndGetId is a helper to create a group and return its ID
func CreateGroupAndGetId(t *testing.T, client *APIClient, name string, memberIds []string) string {
	t.Helper()
	req := CreateGroupRequest{
		Name:      name,
		MemberIds: memberIds,
	}
	resp, err := client.POST("/group/create", req)
	if err != nil {
		t.Fatalf("create group failed: %v", err)
	}
	AssertSuccess(t, resp, "create group should succeed")

	var groupInfo GroupInfo
	if err := resp.ParseData(&groupInfo); err != nil {
		t.Fatalf("parse group info failed: %v", err)
	}
	return groupInfo.Id
}
