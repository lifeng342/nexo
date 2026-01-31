package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/mbeoliero/nexo/internal/middleware"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/mbeoliero/nexo/pkg/response"
)

// GroupHandler handles group-related requests
type GroupHandler struct {
	groupService *service.GroupService
}

// NewGroupHandler creates a new GroupHandler
func NewGroupHandler(groupService *service.GroupService) *GroupHandler {
	return &GroupHandler{groupService: groupService}
}

// CreateGroup handles create group request
func (h *GroupHandler) CreateGroup(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserId(c)
	if userId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrUnauthorized)
		return
	}

	var req service.CreateGroupRequest
	if err := c.BindAndValidate(&req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	group, err := h.groupService.CreateGroup(ctx, userId, &req)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, group)
}

// JoinGroupRequest represents join group request
type JoinGroupRequest struct {
	GroupId   string `json:"group_id"`
	InviterId string `json:"inviter_id,omitempty"`
}

// JoinGroup handles join group request
func (h *GroupHandler) JoinGroup(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserId(c)
	if userId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrUnauthorized)
		return
	}

	var req JoinGroupRequest
	if err := c.BindAndValidate(&req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	if err := h.groupService.JoinGroup(ctx, req.GroupId, userId, req.InviterId); err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, nil)
}

// QuitGroupRequest represents quit group request
type QuitGroupRequest struct {
	GroupId string `json:"group_id"`
}

// QuitGroup handles quit group request
func (h *GroupHandler) QuitGroup(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserId(c)
	if userId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrUnauthorized)
		return
	}

	var req QuitGroupRequest
	if err := c.BindAndValidate(&req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	if err := h.groupService.QuitGroup(ctx, req.GroupId, userId); err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, nil)
}

// GetGroupInfo handles get group info request
func (h *GroupHandler) GetGroupInfo(ctx context.Context, c *app.RequestContext) {
	groupId := c.Query("group_id")
	if groupId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	groupInfo, err := h.groupService.GetGroupInfo(ctx, groupId)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, groupInfo)
}

// GetGroupMembers handles get group members request
func (h *GroupHandler) GetGroupMembers(ctx context.Context, c *app.RequestContext) {
	groupId := c.Query("group_id")
	if groupId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	members, err := h.groupService.GetGroupMembers(ctx, groupId)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, members)
}
