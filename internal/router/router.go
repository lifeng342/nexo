package router

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/hertz-contrib/websocket"
	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/internal/gateway"
	"github.com/mbeoliero/nexo/internal/handler"
	"github.com/mbeoliero/nexo/internal/middleware"
)

// SetupRouter sets up all routes
func SetupRouter(h *server.Hertz, handlers *Handlers, wsServer *gateway.WsServer) {
	cfg := config.GlobalConfig

	// CORS middleware
	h.Use(middleware.CORS())

	// Health check
	h.GET("/health", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]string{"status": "ok"})
	})

	// Auth routes (no auth required)
	authGroup := h.Group("/auth")
	{
		authGroup.POST("/register", handlers.Auth.Register)
		authGroup.POST("/login", handlers.Auth.Login)
	}

	// User routes (auth required)
	userGroup := h.Group("/user", middleware.JWTAuth())
	{
		userGroup.GET("/info", handlers.User.GetUserInfo)
		userGroup.GET("/info/:user_id", handlers.User.GetUserInfoById)
		userGroup.PUT("/update", handlers.User.UpdateUserInfo)
	}

	// Group routes (auth required)
	groupGroup := h.Group("/group", middleware.JWTAuth())
	{
		groupGroup.POST("/create", handlers.Group.CreateGroup)
		groupGroup.POST("/join", handlers.Group.JoinGroup)
		groupGroup.POST("/quit", handlers.Group.QuitGroup)
		groupGroup.GET("/info", handlers.Group.GetGroupInfo)
		groupGroup.GET("/members", handlers.Group.GetGroupMembers)
	}

	// Message routes (auth required)
	msgGroup := h.Group("/msg", middleware.JWTAuth())
	{
		msgGroup.POST("/send", handlers.Message.SendMessage)
		msgGroup.GET("/pull", handlers.Message.PullMessages)
		msgGroup.GET("/max_seq", handlers.Message.GetMaxSeq)
	}

	// Conversation routes (auth required)
	convGroup := h.Group("/conversation", middleware.JWTAuth())
	{
		convGroup.GET("/list", handlers.Conversation.GetConversationList)
		convGroup.GET("/info", handlers.Conversation.GetConversation)
		convGroup.PUT("/update", handlers.Conversation.UpdateConversation)
		convGroup.POST("/mark_read", handlers.Conversation.MarkRead)
		convGroup.GET("/max_read_seq", handlers.Conversation.GetMaxReadSeq)
		convGroup.GET("/unread_count", handlers.Conversation.GetUnreadCount)
	}

	// WebSocket route using hertz-contrib/websocket with proper origin validation
	allowedOrigins := cfg.Server.AllowedOrigins
	upgrader := &websocket.HertzUpgrader{
		CheckOrigin: func(ctx *app.RequestContext) bool {
			return checkOrigin(ctx, allowedOrigins)
		},
	}

	h.GET("/ws", func(ctx context.Context, c *app.RequestContext) {
		wsServer.HandleHertzConnection(ctx, c, upgrader)
	})
}

// checkOrigin validates the Origin header against allowed origins
func checkOrigin(ctx *app.RequestContext, allowedOrigins []string) bool {
	origin := string(ctx.Request.Header.Peek("Origin"))

	// If no origin header, allow (same-origin request or non-browser client)
	if origin == "" {
		return true
	}

	// If no allowed origins configured, reject all cross-origin requests in production
	if len(allowedOrigins) == 0 {
		return false
	}

	// Check against allowed origins
	for _, allowed := range allowedOrigins {
		if allowed == "*" {
			// Wildcard - allow all (only use in development!)
			return true
		}
		if strings.EqualFold(origin, allowed) {
			return true
		}
	}

	return false
}

// Handlers holds all HTTP handlers
type Handlers struct {
	Auth         *handler.AuthHandler
	User         *handler.UserHandler
	Group        *handler.GroupHandler
	Message      *handler.MessageHandler
	Conversation *handler.ConversationHandler
}
