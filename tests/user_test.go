package tests

import (
	"testing"
)

func TestUser_GetInfo(t *testing.T) {
	userId := generateUserId("user_info")
	client, _ := RegisterAndLogin(t, userId, "User Info Test", "password123")

	t.Run("get current user info", func(t *testing.T) {
		resp, err := client.GET("/user/info")
		if err != nil {
			t.Fatalf("get user info failed: %v", err)
		}

		AssertSuccess(t, resp, "get user info should succeed")

		var userInfo UserInfo
		if err := resp.ParseData(&userInfo); err != nil {
			t.Fatalf("parse user info failed: %v", err)
		}

		if userInfo.Id != userId {
			t.Errorf("expected user_id=%s, got %s", userId, userInfo.Id)
		}
		if userInfo.Nickname != "User Info Test" {
			t.Errorf("expected nickname=User Info Test, got %s", userInfo.Nickname)
		}
	})

	t.Run("get other user info by id", func(t *testing.T) {
		// Create another user
		otherUserId := generateUserId("other_user")
		otherClient, _ := RegisterAndLogin(t, otherUserId, "Other User", "password123")
		_ = otherClient // Just to create the user

		resp, err := client.GET("/user/profile/" + otherUserId)
		if err != nil {
			t.Fatalf("get other user info failed: %v", err)
		}

		AssertSuccess(t, resp, "get other user info should succeed")

		var userInfo UserInfo
		if err := resp.ParseData(&userInfo); err != nil {
			t.Fatalf("parse user info failed: %v", err)
		}

		if userInfo.Id != otherUserId {
			t.Errorf("expected user_id=%s, got %s", otherUserId, userInfo.Id)
		}
	})

	t.Run("get non-existent user info", func(t *testing.T) {
		resp, err := client.GET("/user/profile/non_existent_user_12345")
		if err != nil {
			t.Fatalf("get user info failed: %v", err)
		}

		AssertError(t, resp, 2006, "should return user not found error")
	})
}

// UpdateUserRequest represents update user request
type UpdateUserRequest struct {
	Nickname string `json:"nickname,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
}

func TestUser_Update(t *testing.T) {
	userId := generateUserId("user_update")
	client, _ := RegisterAndLogin(t, userId, "Original Name", "password123")

	t.Run("update nickname", func(t *testing.T) {
		req := UpdateUserRequest{
			Nickname: "Updated Name",
		}

		resp, err := client.PUT("/user/update", req)
		if err != nil {
			t.Fatalf("update user failed: %v", err)
		}

		AssertSuccess(t, resp, "update user should succeed")

		// Verify the update
		resp, err = client.GET("/user/info")
		if err != nil {
			t.Fatalf("get user info failed: %v", err)
		}

		var userInfo UserInfo
		if err := resp.ParseData(&userInfo); err != nil {
			t.Fatalf("parse user info failed: %v", err)
		}

		if userInfo.Nickname != "Updated Name" {
			t.Errorf("expected nickname=Updated Name, got %s", userInfo.Nickname)
		}
	})

	t.Run("update avatar", func(t *testing.T) {
		req := UpdateUserRequest{
			Avatar: "https://example.com/avatar.png",
		}

		resp, err := client.PUT("/user/update", req)
		if err != nil {
			t.Fatalf("update user failed: %v", err)
		}

		AssertSuccess(t, resp, "update avatar should succeed")

		// Verify the update
		resp, err = client.GET("/user/info")
		if err != nil {
			t.Fatalf("get user info failed: %v", err)
		}

		var userInfo UserInfo
		if err := resp.ParseData(&userInfo); err != nil {
			t.Fatalf("parse user info failed: %v", err)
		}

		if userInfo.Avatar != "https://example.com/avatar.png" {
			t.Errorf("expected avatar=https://example.com/avatar.png, got %s", userInfo.Avatar)
		}
	})
}
