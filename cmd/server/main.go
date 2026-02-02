package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/mbeoliero/kit/log"
	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/internal/gateway"
	"github.com/mbeoliero/nexo/internal/handler"
	"github.com/mbeoliero/nexo/internal/repository"
	"github.com/mbeoliero/nexo/internal/router"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/constant"
)

func main() {
	ctx := context.TODO()

	// Load configuration
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.CtxError(ctx, "failed to load config: %v", err)
		panic(err)
	}

	log.CtxInfo(ctx, "config loaded: mode=%s", cfg.Server.Mode)

	// Initialize Redis key prefix
	constant.InitRedisKeyPrefix(cfg.Redis.KeyPrefix)
	log.CtxInfo(ctx, "redis key prefix: %s", constant.GetRedisKeyPrefix())

	// Initialize repositories
	repos, err := repository.NewRepositories(cfg)
	if err != nil {
		log.CtxError(ctx, "failed to initialize repositories: %v", err)
		panic(err)
	}
	defer repos.Close()

	// Check database connection
	if err := repos.CheckConnection(ctx); err != nil {
		log.CtxError(ctx, "database connection check failed: %v", err)
		panic(err)
	}
	log.CtxInfo(ctx, "database connection established")

	// Initialize services
	authService := service.NewAuthService(repos.User, cfg, repos.Redis)
	userService := service.NewUserService(repos.User)
	groupService := service.NewGroupService(repos)
	msgService := service.NewMessageService(repos)
	convService := service.NewConversationService(repos)

	// Initialize WebSocket server
	wsServer := gateway.NewWsServer(cfg, repos.Redis, msgService, convService)

	// Set message pusher for message service
	msgService.SetPusher(wsServer)

	// Start WebSocket server
	wsServer.Run(ctx)
	log.CtxInfo(ctx, "websocket server started")

	// Initialize handlers
	handlers := &router.Handlers{
		Auth:         handler.NewAuthHandler(authService),
		User:         handler.NewUserHandler(userService, wsServer),
		Group:        handler.NewGroupHandler(groupService),
		Message:      handler.NewMessageHandler(msgService),
		Conversation: handler.NewConversationHandler(convService),
	}

	// Create Hertz server
	h := server.Default(
		server.WithHostPorts(fmt.Sprintf(":%d", cfg.Server.HTTPPort)),
	)

	// Setup routes
	router.SetupRouter(h, handlers, wsServer)

	log.CtxInfo(ctx, "server starting on port %d", cfg.Server.HTTPPort)

	// Start server in goroutine
	go func() {
		h.Spin()
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.CtxInfo(ctx, "shutting down server...")

	// Graceful shutdown
	if err := h.Shutdown(ctx); err != nil {
		log.CtxError(ctx, "server shutdown error: %v", err)
	}

	log.CtxInfo(ctx, "server stopped")
}
