package sdk

import "context"

// Register registers a new user
func (c *Client) Register(ctx context.Context, req *RegisterRequest) (*UserInfo, error) {
	var result UserInfo
	if err := c.post(ctx, "/auth/register", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Login authenticates a user and returns a token
// The token is automatically stored in the client for subsequent requests
func (c *Client) Login(ctx context.Context, req *LoginRequest) (*LoginResponse, error) {
	var result LoginResponse
	if err := c.post(ctx, "/auth/login", req, &result); err != nil {
		return nil, err
	}
	// Auto-set token for subsequent requests
	c.SetToken(result.Token)
	return &result, nil
}

// LoginWithUserId is a convenience method to login with user Id, password and platform Id
func (c *Client) LoginWithUserId(ctx context.Context, userId, password string, platformId int) (*LoginResponse, error) {
	return c.Login(ctx, &LoginRequest{
		UserId:     userId,
		Password:   password,
		PlatformId: platformId,
	})
}
