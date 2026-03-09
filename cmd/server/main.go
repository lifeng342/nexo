package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/google/uuid"
	"github.com/mbeoliero/kit/log"

	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/internal/gateway"
	"github.com/mbeoliero/nexo/internal/handler"
	"github.com/mbeoliero/nexo/internal/repository"
	"github.com/mbeoliero/nexo/internal/router"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/constant"
)

type serverDependencies struct {
	InstanceID              string
	Gate                    gateway.LifecycleGate
	Presence                *service.PresenceService
	MessageHandler          *handler.MessageHandler
	UserHandler             *handler.UserHandler
	WsServer                *gateway.WsServer
	Handlers                *router.Handlers
	Repositories            *repository.Repositories
	MessageService          *service.MessageService
	ConversationSvc         *service.ConversationService
	RouteStore              *gateway.RouteStore
	InstanceManager         *gateway.InstanceManager
	PushBus                 gateway.PushBus
	PushCoordinator         *gateway.PushCoordinator
	WsServerSnapshotRouteConns func() []gateway.RouteConn
}

func buildServerDependencies(cfg *config.Config, repos *repository.Repositories) (*serverDependencies, error) {
	gate := gateway.NewLifecycleGate()
	instanceID := cfg.WebSocket.CrossInstance.InstanceID
	if instanceID == "" {
		instanceID = fmt.Sprintf("%s-%d-%s", "server", cfg.Server.HTTPPort, uuid.NewString()[:8])
	}

	var authService *service.AuthService
	var userService *service.UserService
	var groupService *service.GroupService
	var msgService *service.MessageService
	var convService *service.ConversationService
	var wsServer *gateway.WsServer
	var presence *service.PresenceService
	var routeStore *gateway.RouteStore
	var instanceManager *gateway.InstanceManager
	var pushBus gateway.PushBus
	var pushCoordinator *gateway.PushCoordinator
	if repos != nil {
		authService = service.NewAuthService(repos.User, cfg, repos.Redis)
		userService = service.NewUserService(repos.User)
		groupService = service.NewGroupService(repos)
		msgService = service.NewMessageService(repos)
		convService = service.NewConversationService(repos)
		wsServer = gateway.NewWsServer(cfg, repos.Redis, msgService, convService, gate)
		if cfg.WebSocket.CrossInstance.Enabled {
			routeStore = gateway.NewRouteStore(repos.Redis, time.Duration(cfg.WebSocket.CrossInstance.RouteTTLSeconds)*time.Second)
			instanceManager = gateway.NewInstanceManager(repos.Redis, instanceID, time.Duration(cfg.WebSocket.CrossInstance.InstanceAliveTTLSeconds)*time.Second)
			pushBus = gateway.NewRedisPushBus(repos.Redis)
			pushCoordinator = gateway.NewPushCoordinator(instanceID, gate, routeStore, pushBus, wsServer)
			wsServer.AttachPushCoordinator(pushCoordinator)
			wsServer.AttachRouteStore(routeStore, instanceID)
			msgService.SetPusher(wsServer)
			presence = service.NewPresenceService(func() map[string][]service.RouteConnRef { return wsServer.LocalPresenceSnapshot(instanceID) }, func(runtimeCtx context.Context, userIDs []string) (map[string][]service.RouteConnRef, error) {
				refs, err := routeStore.GetUsersPresenceConnRefs(runtimeCtx, userIDs)
				if err != nil {
					return nil, err
				}
				result := make(map[string][]service.RouteConnRef, len(refs))
				for userID, conns := range refs {
					mapped := make([]service.RouteConnRef, 0, len(conns))
					for _, conn := range conns {
						mapped = append(mapped, service.RouteConnRef{UserId: conn.UserId, ConnId: conn.ConnId, InstanceId: conn.InstanceId, PlatformId: conn.PlatformId})
					}
					result[userID] = mapped
				}
				return result, nil
			}, instanceID)
		} else {
			msgService.SetPusher(wsServer)
			presence = service.NewPresenceService(func() map[string][]service.RouteConnRef { return wsServer.LocalPresenceSnapshot(instanceID) }, nil, instanceID)
		}
	} else {
		presence = service.NewPresenceService(func() map[string][]service.RouteConnRef { return map[string][]service.RouteConnRef{} }, nil, instanceID)
	}

	messageHandler := handler.NewMessageHandler(msgService, gate)
	userHandler := handler.NewUserHandler(userService, presence)
	handlers := &router.Handlers{
		Auth:         handler.NewAuthHandler(authService),
		User:         userHandler,
		Group:        handler.NewGroupHandler(groupService),
		Message:      messageHandler,
		Conversation: handler.NewConversationHandler(convService),
	}

	return &serverDependencies{InstanceID: instanceID, Gate: gate, Presence: presence, MessageHandler: messageHandler, UserHandler: userHandler, WsServer: wsServer, Handlers: handlers, Repositories: repos, MessageService: msgService, ConversationSvc: convService, RouteStore: routeStore, InstanceManager: instanceManager, PushBus: pushBus, PushCoordinator: pushCoordinator, WsServerSnapshotRouteConns: func() []gateway.RouteConn {
		if wsServer == nil {
			return nil
		}
		return wsServerSnapshotRouteConns(wsServer, instanceID)
	}}, nil
}

func wsServerSnapshotRouteConns(wsServer *gateway.WsServer, instanceID string) []gateway.RouteConn {
	if wsServer == nil {
		return nil
	}
	return wsServer.SnapshotRouteConns(instanceID)
}

func waitForSendDrain(runtimeCtx context.Context, gate gateway.LifecycleGate) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if gate.Snapshot().InflightSend == 0 {
			return
		}
		select {
		case <-runtimeCtx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runServerShutdown(runtimeCtx context.Context, runtimeCancel context.CancelFunc, h *server.Hertz, deps *serverDependencies, cfg *config.Config, forceUnrouteable bool) {
	log.CtxInfo(runtimeCtx, "shutting down server...")
	deps.Gate.MarkUnready()
	if forceUnrouteable {
		deps.Gate.MarkUnrouteable()
	}
	deps.Gate.CloseIngress()
	deps.Gate.BeginSendDrain()
	deps.Gate.EnterDraining()
	if deps.InstanceManager != nil {
		if err := deps.InstanceManager.UpdateState(runtimeCtx, deps.InstanceManager.SnapshotState(deps.Gate)); err != nil {
			log.CtxWarn(runtimeCtx, "failed to publish instance state during shutdown: instance_id=%s, error=%v", deps.InstanceID, err)
		}
	}

	httpShutdownCtx, httpCancel := context.WithTimeout(context.Background(), time.Duration(cfg.WebSocket.CrossInstance.DrainTimeoutSeconds)*time.Second)
	defer httpCancel()
	if err := h.Shutdown(httpShutdownCtx); err != nil {
		log.CtxError(runtimeCtx, "server shutdown error: %v", err)
	}

	waitForSendDrain(httpShutdownCtx, deps.Gate)
	deps.Gate.CloseSendPath()
	if deps.Gate.Snapshot().InflightSend > 0 {
		deps.Gate.ForceCloseSendPath()
	}

	if !forceUnrouteable {
		grace := time.Duration(cfg.WebSocket.CrossInstance.DrainRouteableGraceSeconds) * time.Second
		if grace > 0 {
			graceCtx, graceCancel := context.WithTimeout(context.Background(), grace)
			defer graceCancel()
			<-graceCtx.Done()
		}
		deps.Gate.MarkUnrouteable()
	}
	if deps.InstanceManager != nil {
		if err := deps.InstanceManager.UpdateState(runtimeCtx, deps.InstanceManager.SnapshotState(deps.Gate)); err != nil {
			log.CtxWarn(runtimeCtx, "failed to publish final instance state during shutdown: instance_id=%s, error=%v", deps.InstanceID, err)
		}
	}
	if deps.PushBus != nil {
		if err := deps.PushBus.Close(); err != nil {
			log.CtxWarn(runtimeCtx, "failed to close push bus: instance_id=%s, error=%v", deps.InstanceID, err)
		}
	}
	runtimeCancel()
	log.CtxInfo(runtimeCtx, "server stopped")
}

func main() {
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	defer runtimeCancel()
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		log.CtxError(runtimeCtx, "failed to load config: %v", err)
		panic(err)
	}

	log.CtxInfo(runtimeCtx, "config loaded: mode=%s", cfg.Server.Mode)
	constant.InitRedisKeyPrefix(cfg.Redis.KeyPrefix)
	log.CtxInfo(runtimeCtx, "redis key prefix: %s", constant.GetRedisKeyPrefix())

	repos, err := repository.NewRepositories(cfg)
	if err != nil {
		log.CtxError(runtimeCtx, "failed to initialize repositories: %v", err)
		panic(err)
	}
	defer func() { _ = repos.Close() }()
	if err = repos.CheckConnection(runtimeCtx); err != nil {
		log.CtxError(runtimeCtx, "database connection check failed: %v", err)
		panic(err)
	}
	log.CtxInfo(runtimeCtx, "database connection established")

	deps, err := buildServerDependencies(cfg, repos)
	if err != nil {
		log.CtxError(runtimeCtx, "failed to build server dependencies: %v", err)
		panic(err)
	}
	if deps.InstanceManager != nil {
		deps.InstanceManager.Start(runtimeCtx, deps.Gate, time.Duration(cfg.WebSocket.CrossInstance.HeartbeatSecond)*time.Second)
		if err := deps.InstanceManager.UpdateState(runtimeCtx, deps.InstanceManager.SnapshotState(deps.Gate)); err != nil {
			log.CtxWarn(runtimeCtx, "failed to publish initial instance state: instance_id=%s, error=%v", deps.InstanceID, err)
		}
	}
	if deps.RouteStore != nil && deps.WsServer != nil {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.WebSocket.CrossInstance.HeartbeatSecond) * time.Second)
			defer ticker.Stop()
			for {
				if err := deps.RouteStore.ReconcileInstanceRoutes(runtimeCtx, deps.InstanceID, deps.WsServerSnapshotRouteConns()); err != nil {
					log.CtxWarn(runtimeCtx, "route reconcile failed: instance_id=%s, error=%v", deps.InstanceID, err)
				}
				select {
				case <-runtimeCtx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	deps.WsServer.Run(runtimeCtx)
	log.CtxInfo(runtimeCtx, "websocket server started")

	h := server.Default(server.WithHostPorts(fmt.Sprintf(":%d", cfg.Server.HTTPPort)))
	router.SetupRouter(h, deps.Handlers, deps.WsServer, deps.Gate)
	log.CtxInfo(runtimeCtx, "server starting on port %d", cfg.Server.HTTPPort)
	go func() { h.Spin() }()

	if deps.PushBus != nil && deps.PushCoordinator != nil {
		if err := deps.PushBus.SubscribeInstance(runtimeCtx, deps.InstanceID, deps.PushCoordinator.OnRemoteEnvelope, func(error) {
			go runServerShutdown(runtimeCtx, runtimeCancel, h, deps, cfg, true)
		}); err != nil {
			log.CtxError(runtimeCtx, "failed to subscribe push bus: %v", err)
			panic(err)
		}
	}

	<-signalCtx.Done()
	runServerShutdown(runtimeCtx, runtimeCancel, h, deps, cfg, false)
}
