package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/mbeoliero/nexo/pkg/response"
)

// AuthHandler handles authentication requests
type AuthHandler struct {
	authService *service.AuthService
}

// NewAuthHandler creates a new AuthHandler
func NewAuthHandler(authService *service.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

// Register handles user registration
func (h *AuthHandler) Register(ctx context.Context, c *app.RequestContext) {
	var req service.RegisterRequest
	if err := c.BindAndValidate(&req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	userInfo, err := h.authService.Register(ctx, &req)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, userInfo)
}

// Login handles user login
func (h *AuthHandler) Login(ctx context.Context, c *app.RequestContext) {
	var req service.LoginRequest
	if err := c.BindAndValidate(&req); err != nil {
		response.ErrorWithCode(ctx, c, errcode.ErrInvalidParam)
		return
	}

	resp, err := h.authService.Login(ctx, &req)
	if err != nil {
		response.Error(ctx, c, err)
		return
	}

	response.Success(ctx, c, resp)
}
