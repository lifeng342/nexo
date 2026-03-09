package handler

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/mbeoliero/nexo/internal/middleware"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/mbeoliero/nexo/pkg/response"
)

type PresenceService interface {
	GetUsersOnlineStatus(ctx context.Context, userIDs []string) ([]*service.OnlineStatusResult, response.Meta, error)
}

// UserHandler handles user-related requests
type UserHandler struct {
	userService *service.UserService
	presence    PresenceService
}

// NewUserHandler creates a new UserHandler
func NewUserHandler(userService *service.UserService, presence PresenceService) *UserHandler {
	return &UserHandler{
		userService: userService,
		presence:    presence,
	}
}

// GetUserInfo handles get user info request
func (h *UserHandler) GetUserInfo(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserId(c)
	if userId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrUnauthorized)
		return
	}

	userInfo, err := h.userService.GetUserInfo(ctx, userId)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, userInfo)
}

// GetUserInfoById handles get user info by Id request
func (h *UserHandler) GetUserInfoById(ctx context.Context, c *app.RequestContext) {
	userId := c.Param("user_id")
	if userId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	userInfo, err := h.userService.GetUserInfo(ctx, userId)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, userInfo)
}

// UpdateUserInfo handles update user info request
func (h *UserHandler) UpdateUserInfo(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserId(c)
	if userId == "" {
		response.ErrorWithCode(ctx, c, errcode.ErrUnauthorized)
		return
	}

	var req service.UpdateUserRequest
	if err := c.BindAndValidate(&req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	userInfo, err := h.userService.UpdateUserInfo(ctx, userId, &req)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, userInfo)
}

// GetUsersInfoReq represents the request for batch getting users' info
type GetUsersInfoReq struct {
	UserIds []string `json:"user_ids" vd:"len($)>0,len($)<=100"`
}

// GetUsersInfo handles batch get users info request
func (h *UserHandler) GetUsersInfo(ctx context.Context, c *app.RequestContext) {
	var req GetUsersInfoReq
	if err := c.BindAndValidate(&req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	userInfos, err := h.userService.GetUserInfos(ctx, req.UserIds)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, userInfos)
}

// GetUsersOnlineStatusReq represents the request for getting users' online status
type GetUsersOnlineStatusReq struct {
	UserIds []string `json:"user_ids" vd:"len($)>0,len($)<=100"`
}

// GetUsersOnlineStatus handles get users online status request
func (h *UserHandler) GetUsersOnlineStatus(ctx context.Context, c *app.RequestContext) {
	var req GetUsersOnlineStatusReq
	if err := json.Unmarshal(c.Request.Body(), &req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}
	if len(req.UserIds) == 0 || len(req.UserIds) > 100 {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	if h.presence == nil {
		response.ErrorWithCode(ctx, c, errcode.ErrServerShuttingDown)
		return
	}
	results, meta, err := h.presence.GetUsersOnlineStatus(ctx, req.UserIds)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}
	if meta != nil {
		response.SuccessWithMeta(ctx, c, results, meta)
		return
	}
	response.Success(ctx, c, results)
}
