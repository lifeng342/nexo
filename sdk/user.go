package sdk

import "context"

// GetUserInfo gets the current user's info
func (c *Client) GetUserInfo(ctx context.Context) (*UserInfo, error) {
	var result UserInfo
	if err := c.get(ctx, "/user/info", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetUserInfoById gets a user's info by Id
func (c *Client) GetUserInfoById(ctx context.Context, userId string) (*UserInfo, error) {
	var result UserInfo
	if err := c.get(ctx, "/user/profile/"+userId, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateUserInfo updates the current user's info
func (c *Client) UpdateUserInfo(ctx context.Context, req *UpdateUserRequest) (*UserInfo, error) {
	var result UserInfo
	if err := c.put(ctx, "/user/update", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetUsersInfo gets multiple users' info by Ids
func (c *Client) GetUsersInfo(ctx context.Context, userIds []string) ([]*UserInfo, error) {
	var result []*UserInfo
	req := &GetUsersInfoRequest{UserIds: userIds}
	if err := c.post(ctx, "/user/batch_info", req, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetUsersOnlineStatus gets online status for multiple users
func (c *Client) GetUsersOnlineStatus(ctx context.Context, userIds []string) ([]*OnlineStatus, error) {
	var result []*OnlineStatus
	req := &GetUsersOnlineStatusRequest{UserIds: userIds}
	if err := c.post(ctx, "/user/get_users_online_status", req, &result); err != nil {
		return nil, err
	}
	return result, nil
}
