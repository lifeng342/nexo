package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/mbeoliero/nexo/internal/middleware"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/mbeoliero/nexo/pkg/response"
)

// UserHandler handles user-related requests
type UserHandler struct {
	userService *service.UserService
}

// NewUserHandler creates a new UserHandler
func NewUserHandler(userService *service.UserService) *UserHandler {
	return &UserHandler{userService: userService}
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
