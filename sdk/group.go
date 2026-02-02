package sdk

import "context"

// CreateGroup creates a new group
func (c *Client) CreateGroup(ctx context.Context, req *CreateGroupRequest) (*GroupInfo, error) {
	var result GroupInfo
	if err := c.post(ctx, "/group/create", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// JoinGroup joins a group
func (c *Client) JoinGroup(ctx context.Context, groupId string, inviterId string) error {
	req := &JoinGroupRequest{
		GroupId:   groupId,
		InviterId: inviterId,
	}
	return c.post(ctx, "/group/join", req, nil)
}

// QuitGroup quits a group
func (c *Client) QuitGroup(ctx context.Context, groupId string) error {
	req := &QuitGroupRequest{GroupId: groupId}
	return c.post(ctx, "/group/quit", req, nil)
}

// GetGroupInfo gets group info
func (c *Client) GetGroupInfo(ctx context.Context, groupId string) (*GroupInfo, error) {
	var result GroupInfo
	params := map[string]string{"group_id": groupId}
	if err := c.get(ctx, "/group/info", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetGroupMembers gets group members
func (c *Client) GetGroupMembers(ctx context.Context, groupId string) ([]*GroupMember, error) {
	var result []*GroupMember
	params := map[string]string{"group_id": groupId}
	if err := c.get(ctx, "/group/members", params, &result); err != nil {
		return nil, err
	}
	return result, nil
}
