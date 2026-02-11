package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/mbeoliero/nexo/internal/gateway"
	"github.com/mbeoliero/nexo/internal/middleware"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/mbeoliero/nexo/pkg/response"
)

// UserHandler handles user-related requests
type UserHandler struct {
	userService *service.UserService
	wsServer    *gateway.WsServer
}

// NewUserHandler creates a new UserHandler
func NewUserHandler(userService *service.UserService, wsServer *gateway.WsServer) *UserHandler {
	return &UserHandler{
		userService: userService,
		wsServer:    wsServer,
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
	UserIds []string `json:"user_ids" vd:"len($)>0"`
}

// GetUsersOnlineStatus handles get users online status request
func (h *UserHandler) GetUsersOnlineStatus(ctx context.Context, c *app.RequestContext) {
	var req GetUsersOnlineStatusReq
	if err := c.BindAndValidate(&req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	results := h.wsServer.GetUsersOnlineStatus(req.UserIds)
	response.Success(ctx, c, results)
}
