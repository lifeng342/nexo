package tests

import (
	"testing"
)

// RegisterRequest represents user registration request
type RegisterRequest struct {
	UserId   string `json:"user_id"`
	Nickname string `json:"nickname"`
	Password string `json:"password"`
	Avatar   string `json:"avatar,omitempty"`
}

// LoginRequest represents user login request
type LoginRequest struct {
	UserId     string `json:"user_id"`
	Password   string `json:"password"`
	PlatformId int    `json:"platform_id"`
}

// LoginResponse represents user login response
type LoginResponse struct {
	Token    string   `json:"token"`
	UserInfo UserInfo `json:"user_info"`
}

// UserInfo represents user info
type UserInfo struct {
	Id       string `json:"id"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
}

func TestAuth_Register(t *testing.T) {
	client := NewAPIClient()
	userId := generateUserId("test_user")

	t.Run("register new user", func(t *testing.T) {
		req := RegisterRequest{
			UserId:   userId,
			Nickname: "Test User",
			Password: "password123",
		}

		resp, err := client.POST("/auth/register", req)
		if err != nil {
			t.Fatalf("register request failed: %v", err)
		}

		AssertSuccess(t, resp, "register should succeed")

		var userInfo UserInfo
		if err := resp.ParseData(&userInfo); err != nil {
			t.Fatalf("parse user info failed: %v", err)
		}

		if userInfo.Id != userId {
			t.Errorf("expected user_id=%s, got %s", userId, userInfo.Id)
		}
		if userInfo.Nickname != "Test User" {
			t.Errorf("expected nickname=Test User, got %s", userInfo.Nickname)
		}
	})

	t.Run("register duplicate user", func(t *testing.T) {
		req := RegisterRequest{
			UserId:   userId,
			Nickname: "Test User 2",
			Password: "password123",
		}

		resp, err := client.POST("/auth/register", req)
		if err != nil {
			t.Fatalf("register request failed: %v", err)
		}

		AssertError(t, resp, 2007, "should return user exists error")
	})

	t.Run("register with empty user_id", func(t *testing.T) {
		req := RegisterRequest{
			UserId:   "",
			Nickname: "Test User",
			Password: "password123",
		}

		resp, err := client.POST("/auth/register", req)
		if err != nil {
			t.Fatalf("register request failed: %v", err)
		}

		// Empty user_id should auto-generate UUID, so it should succeed
		AssertSuccess(t, resp, "register with empty user_id should succeed")
	})
}

func TestAuth_Login(t *testing.T) {
	client := NewAPIClient()
	userId := generateUserId("login_user")
	password := "password123"

	// First register a user
	registerReq := RegisterRequest{
		UserId:   userId,
		Nickname: "Login Test User",
		Password: password,
	}
	resp, err := client.POST("/auth/register", registerReq)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	AssertSuccess(t, resp, "register should succeed")

	t.Run("login with correct password", func(t *testing.T) {
		req := LoginRequest{
			UserId:     userId,
			Password:   password,
			PlatformId: 5, // Web
		}

		resp, err := client.POST("/auth/login", req)
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}

		AssertSuccess(t, resp, "login should succeed")

		var loginResp LoginResponse
		if err := resp.ParseData(&loginResp); err != nil {
			t.Fatalf("parse login response failed: %v", err)
		}

		if loginResp.Token == "" {
			t.Error("token should not be empty")
		}
		if loginResp.UserInfo.Id != userId {
			t.Errorf("expected user_id=%s, got %s", userId, loginResp.UserInfo.Id)
		}
	})

	t.Run("login with wrong password", func(t *testing.T) {
		req := LoginRequest{
			UserId:     userId,
			Password:   "wrongpassword",
			PlatformId: 5,
		}

		resp, err := client.POST("/auth/login", req)
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}

		AssertError(t, resp, 2008, "should return password wrong error")
	})

	t.Run("login with non-existent user", func(t *testing.T) {
		req := LoginRequest{
			UserId:     "non_existent_user",
			Password:   password,
			PlatformId: 5,
		}

		resp, err := client.POST("/auth/login", req)
		if err != nil {
			t.Fatalf("login request failed: %v", err)
		}

		AssertError(t, resp, 2006, "should return user not found error")
	})

	t.Run("login on different platforms", func(t *testing.T) {
		platforms := []int{1, 2, 5} // iOS, Android, Web
		tokens := make([]string, len(platforms))

		for i, platformId := range platforms {
			req := LoginRequest{
				UserId:     userId,
				Password:   password,
				PlatformId: platformId,
			}

			resp, err := client.POST("/auth/login", req)
			if err != nil {
				t.Fatalf("login request failed for platform %d: %v", platformId, err)
			}

			AssertSuccess(t, resp, "login should succeed for platform %d", platformId)

			var loginResp LoginResponse
			if err := resp.ParseData(&loginResp); err != nil {
				t.Fatalf("parse login response failed: %v", err)
			}

			tokens[i] = loginResp.Token
		}

		// All tokens should be different
		for i := 0; i < len(tokens); i++ {
			for j := i + 1; j < len(tokens); j++ {
				if tokens[i] == tokens[j] {
					t.Errorf("tokens for different platforms should be different")
				}
			}
		}
	})
}

func TestAuth_TokenValidation(t *testing.T) {
	client := NewAPIClient()
	userId := generateUserId("token_user")
	password := "password123"

	// Register and login
	registerReq := RegisterRequest{
		UserId:   userId,
		Nickname: "Token Test User",
		Password: password,
	}
	resp, err := client.POST("/auth/register", registerReq)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	AssertSuccess(t, resp, "register should succeed")

	loginReq := LoginRequest{
		UserId:     userId,
		Password:   password,
		PlatformId: 5,
	}
	resp, err = client.POST("/auth/login", loginReq)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	AssertSuccess(t, resp, "login should succeed")

	var loginResp LoginResponse
	if err := resp.ParseData(&loginResp); err != nil {
		t.Fatalf("parse login response failed: %v", err)
	}

	t.Run("access protected endpoint with valid token", func(t *testing.T) {
		client.SetToken(loginResp.Token)
		resp, err := client.GET("/user/info")
		if err != nil {
			t.Fatalf("get user info failed: %v", err)
		}

		AssertSuccess(t, resp, "should access with valid token")
	})

	t.Run("access protected endpoint without token", func(t *testing.T) {
		noTokenClient := NewAPIClient()
		resp, err := noTokenClient.GET("/user/info")
		if err != nil {
			t.Fatalf("get user info failed: %v", err)
		}

		AssertError(t, resp, 2003, "should return token missing error")
	})

	t.Run("access protected endpoint with invalid token", func(t *testing.T) {
		invalidClient := NewAPIClient()
		invalidClient.SetToken("invalid_token")
		resp, err := invalidClient.GET("/user/info")
		if err != nil {
			t.Fatalf("get user info failed: %v", err)
		}

		// Should return unauthorized or token invalid error
		if resp.Code != 1003 && resp.Code != 2001 {
			t.Errorf("expected code 1003 or 2001, got %d", resp.Code)
		}
	})
}

// RegisterAndLogin is a helper function to register and login a user
func RegisterAndLogin(t *testing.T, userId, nickname, password string) (*APIClient, string) {
	t.Helper()
	client := NewAPIClient()

	// Register
	registerReq := RegisterRequest{
		UserId:   userId,
		Nickname: nickname,
		Password: password,
	}
	resp, err := client.POST("/auth/register", registerReq)
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	// Ignore if user already exists
	if resp.Code != 0 && resp.Code != 2007 {
		t.Fatalf("register failed: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	// Login
	loginReq := LoginRequest{
		UserId:     userId,
		Password:   password,
		PlatformId: 5,
	}
	resp, err = client.POST("/auth/login", loginReq)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	AssertSuccess(t, resp, "login should succeed")

	var loginResp LoginResponse
	if err := resp.ParseData(&loginResp); err != nil {
		t.Fatalf("parse login response failed: %v", err)
	}

	client.SetToken(loginResp.Token)
	return client, loginResp.Token
}
