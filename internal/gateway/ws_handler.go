package gateway

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
	"github.com/hertz-contrib/websocket"
	"github.com/mbeoliero/kit/log"
	"github.com/mbeoliero/nexo/pkg/jwt"
)

// HandleHertzConnection handles a WebSocket connection from Hertz using hertz-contrib/websocket
func (s *WsServer) HandleHertzConnection(ctx context.Context, c *app.RequestContext, upgrader *websocket.HertzUpgrader) {
	// Check connection limit
	if s.onlineConnNum.Load() >= s.maxConnNum {
		c.String(503, "connection limit exceeded")
		return
	}

	// Parse query parameters
	token := string(c.Query("token"))
	sendId := string(c.Query("send_id"))
	platformIdStr := string(c.Query("platform_id"))
	sdkType := string(c.Query("sdk_type"))

	if token == "" || sendId == "" {
		c.String(400, "missing required parameters")
		return
	}

	platformId := 0
	if platformIdStr != "" {
		platformId, _ = strconv.Atoi(platformIdStr)
	}

	// Validate token
	claims, err := jwt.ValidateToken(token, s.cfg.JWT.Secret, sendId, platformId)
	if err != nil {
		log.CtxDebug(ctx, "token validation failed: send_id=%s, error=%v", sendId, err)
		c.String(401, "unauthorized")
		return
	}

	// Upgrade connection using hertz-contrib/websocket
	err = upgrader.Upgrade(c, func(conn *websocket.Conn) {
		// Create client using the hertz websocket connection
		connId := uuid.New().String()
		wsConn := NewHertzWebSocketClientConn(conn, s.cfg.WebSocket.MaxMessageSize, PongWait, PingPeriod)
		client := NewClient(wsConn, claims.UserId, claims.PlatformId, sdkType, token, connId, s)

		// Register client
		s.registerChan <- client

		// Start client (blocking - handles message loop)
		client.readLoop()
	})

	if err != nil {
		log.CtxWarn(ctx, "websocket upgrade failed: %v", err)
		return
	}
}
