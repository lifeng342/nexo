package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/mbeoliero/kit/log"
	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/mbeoliero/nexo/pkg/jwt"
	"github.com/redis/go-redis/v9"
)

// WsServer is the WebSocket server
type WsServer struct {
	upgrader         *websocket.Upgrader
	cfg              *config.Config
	userMap          *UserMap
	registerChan     chan *Client
	unregisterChan   chan *Client
	pushChan         chan *PushTask
	msgService       *service.MessageService
	convService      *service.ConversationService
	onlineUserNum    atomic.Int64
	onlineConnNum    atomic.Int64
	maxConnNum       int64
}

// PushTask represents a message push task
type PushTask struct {
	Msg        *entity.Message
	TargetIds  []string
	ExcludeId  string // Exclude specific connection Id
}

// NewWsServer creates a new WebSocket server
func NewWsServer(cfg *config.Config, rdb *redis.Client, msgService *service.MessageService, convService *service.ConversationService) *WsServer {
	upgrader := &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	server := &WsServer{
		upgrader:       upgrader,
		cfg:            cfg,
		userMap:        NewUserMap(rdb),
		registerChan:   make(chan *Client, 1000),
		unregisterChan: make(chan *Client, 1000),
		pushChan:       make(chan *PushTask, cfg.WebSocket.PushChannelSize),
		msgService:     msgService,
		convService:    convService,
		maxConnNum:     cfg.WebSocket.MaxConnNum,
	}

	return server
}

// Run starts the WebSocket server
func (s *WsServer) Run(ctx context.Context) {
	// Start event loop
	go s.eventLoop(ctx)
	// Start push workers
	workerNum := s.cfg.WebSocket.PushWorkerNum
	if workerNum <= 0 {
		workerNum = 10
	}
	for i := 0; i < workerNum; i++ {
		go s.pushLoop(ctx)
	}
	log.Info("started %d push workers", workerNum)
}

// eventLoop handles client registration and unregistration
func (s *WsServer) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case client := <-s.registerChan:
			s.registerClient(ctx, client)
		case client := <-s.unregisterChan:
			s.unregisterClient(ctx, client)
		}
	}
}

// pushLoop handles async message pushing
func (s *WsServer) pushLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-s.pushChan:
			s.processPushTask(ctx, task)
		}
	}
}

// processPushTask processes a single push task
func (s *WsServer) processPushTask(ctx context.Context, task *PushTask) {
	msgData := s.messageToMsgData(task.Msg)

	for _, userId := range task.TargetIds {
		clients, ok := s.userMap.GetAll(userId)
		if !ok {
			continue
		}

		for _, client := range clients {
			// Skip excluded connection
			if task.ExcludeId != "" && client.ConnId == task.ExcludeId {
				continue
			}

			if err := client.PushMessage(ctx, msgData); err != nil {
				log.CtxDebug(ctx, "push to client failed: user_id=%s, conn_id=%s, error=%v", userId, client.ConnId, err)
			}
		}
	}
}

// registerClient registers a client
func (s *WsServer) registerClient(ctx context.Context, client *Client) {
	existingClients, exists := s.userMap.GetAll(client.UserId)
	if !exists {
		s.onlineUserNum.Add(1)
	}

	s.userMap.Register(ctx, client)
	s.onlineConnNum.Add(1)

	log.CtxInfo(ctx, "client registered: user_id=%s, platform_id=%d, conn_id=%s, existing_conns=%d, online_users=%d, online_conns=%d",
		client.UserId, client.PlatformId, client.ConnId, len(existingClients), s.onlineUserNum.Load(), s.onlineConnNum.Load())
}

// unregisterClient unregisters a client
func (s *WsServer) unregisterClient(ctx context.Context, client *Client) {
	isUserOffline := s.userMap.Unregister(ctx, client)
	s.onlineConnNum.Add(-1)

	if isUserOffline {
		s.onlineUserNum.Add(-1)
	}

	log.CtxInfo(ctx, "client unregistered: user_id=%s, platform_id=%d, conn_id=%s, user_offline=%v, online_users=%d, online_conns=%d",
		client.UserId, client.PlatformId, client.ConnId, isUserOffline, s.onlineUserNum.Load(), s.onlineConnNum.Load())
}

// UnregisterClient queues client for unregistration
func (s *WsServer) UnregisterClient(client *Client) {
	select {
	case s.unregisterChan <- client:
	default:
		log.Warn("unregister channel full: user_id=%s", client.UserId)
	}
}

// HandleConnection handles a new WebSocket connection (Hertz handler)
func (s *WsServer) HandleConnection(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	// Check connection limit
	if s.onlineConnNum.Load() >= s.maxConnNum {
		http.Error(w, "connection limit exceeded", http.StatusServiceUnavailable)
		return
	}

	// Parse query parameters
	token := r.URL.Query().Get(QueryToken)
	sendId := r.URL.Query().Get(QuerySendId)
	platformIdStr := r.URL.Query().Get(QueryPlatformId)
	sdkType := r.URL.Query().Get(QuerySDKType)

	if token == "" || sendId == "" {
		http.Error(w, "missing required parameters", http.StatusBadRequest)
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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Upgrade connection
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.CtxWarn(ctx, "websocket upgrade failed: %v", err)
		return
	}

	// Create client
	connId := uuid.New().String()
	wsConn := NewWebSocketClientConn(conn, s.cfg.WebSocket.MaxMessageSize, PongWait, PingPeriod)
	client := NewClient(wsConn, claims.UserId, claims.PlatformId, sdkType, token, connId, s)

	// Register client
	s.registerChan <- client

	// Start client
	client.Start()
}

// AsyncPushToUsers queues a message push to users
func (s *WsServer) AsyncPushToUsers(msg *entity.Message, userIds []string, excludeConnId string) {
	task := &PushTask{
		Msg:       msg,
		TargetIds: userIds,
		ExcludeId: excludeConnId,
	}

	select {
	case s.pushChan <- task:
		// Successfully queued
	default:
		// Queue full, log warning
		log.Warn("push channel full, message dropped: conversation_id=%s, seq=%d", msg.ConversationId, msg.Seq)
	}
}

// GetOnlineUserCount returns online user count
func (s *WsServer) GetOnlineUserCount() int64 {
	return s.onlineUserNum.Load()
}

// GetOnlineConnCount returns online connection count
func (s *WsServer) GetOnlineConnCount() int64 {
	return s.onlineConnNum.Load()
}


// messageToMsgData converts entity.Message to MessageData
func (s *WsServer) messageToMsgData(msg *entity.Message) *MessageData {
	custom := ""
	if msg.ContentCustom != nil {
		custom = *msg.ContentCustom
	}
	return &MessageData{
		ServerMsgId:    msg.Id,
		ConversationId: msg.ConversationId,
		Seq:            msg.Seq,
		ClientMsgId:    msg.ClientMsgId,
		SenderId:       msg.SenderId,
		RecvId:         msg.RecvId,
		GroupId:        msg.GroupId,
		SessionType:    msg.SessionType,
		MsgType:        msg.MsgType,
		Content: struct {
			Text   string `json:"text,omitempty"`
			Image  string `json:"image,omitempty"`
			Video  string `json:"video,omitempty"`
			Audio  string `json:"audio,omitempty"`
			File   string `json:"file,omitempty"`
			Custom string `json:"custom,omitempty"`
		}{
			Text:   msg.ContentText,
			Image:  msg.ContentImage,
			Video:  msg.ContentVideo,
			Audio:  msg.ContentAudio,
			File:   msg.ContentFile,
			Custom: custom,
		},
		SendAt: msg.SendAt,
	}
}

// ========== Message Handlers ==========

// HandleGetNewestSeq handles get newest seq request
func (s *WsServer) HandleGetNewestSeq(ctx context.Context, client *Client, req *WSRequest) ([]byte, error) {
	var getSeqReq GetNewestSeqReq
	if err := json.Unmarshal(req.Data, &getSeqReq); err != nil {
		return nil, errcode.ErrInvalidParam
	}

	seqs := make(map[string]int64)
	for _, convId := range getSeqReq.ConversationIds {
		maxSeq, _ := s.msgService.GetMaxSeq(ctx, client.UserId, convId)
		seqs[convId] = maxSeq
	}

	resp := GetNewestSeqResp{Seqs: seqs}
	return json.Marshal(resp)
}

// HandleSendMsg handles send message request
func (s *WsServer) HandleSendMsg(ctx context.Context, client *Client, req *WSRequest) ([]byte, error) {
	var sendReq SendMsgReq
	if err := json.Unmarshal(req.Data, &sendReq); err != nil {
		return nil, errcode.ErrInvalidParam
	}

	// Build service request
	svcReq := &service.SendMessageRequest{
		ClientMsgId: sendReq.ClientMsgId,
		RecvId:      sendReq.RecvId,
		GroupId:     sendReq.GroupId,
		SessionType: sendReq.SessionType,
		MsgType:     sendReq.MsgType,
		Content: entity.MessageContent{
			Text:   sendReq.Content.Text,
			Image:  sendReq.Content.Image,
			Video:  sendReq.Content.Video,
			Audio:  sendReq.Content.Audio,
			File:   sendReq.Content.File,
			Custom: sendReq.Content.Custom,
		},
	}

	msg, err := s.msgService.SendMessage(ctx, client.UserId, svcReq)
	if err != nil {
		return nil, err
	}

	resp := SendMsgResp{
		ServerMsgId:    msg.Id,
		ConversationId: msg.ConversationId,
		Seq:            msg.Seq,
		ClientMsgId:    msg.ClientMsgId,
		SendAt:         msg.SendAt,
	}

	return json.Marshal(resp)
}

// HandlePullMsg handles pull messages request
func (s *WsServer) HandlePullMsg(ctx context.Context, client *Client, req *WSRequest) ([]byte, error) {
	var pullReq PullMsgReq
	if err := json.Unmarshal(req.Data, &pullReq); err != nil {
		return nil, errcode.ErrInvalidParam
	}

	svcReq := &service.PullMessagesRequest{
		ConversationId: pullReq.ConversationId,
		BeginSeq:       pullReq.BeginSeq,
		EndSeq:         pullReq.EndSeq,
		Limit:          pullReq.Limit,
	}

	messages, maxSeq, err := s.msgService.PullMessages(ctx, client.UserId, svcReq)
	if err != nil {
		return nil, err
	}

	msgDataList := make([]*MessageData, 0, len(messages))
	for _, msg := range messages {
		msgDataList = append(msgDataList, s.messageToMsgData(msg))
	}

	resp := PullMsgResp{
		Messages: msgDataList,
		MaxSeq:   maxSeq,
	}

	return json.Marshal(resp)
}

// HandlePullMsgBySeqList handles pull messages by seq list request
func (s *WsServer) HandlePullMsgBySeqList(ctx context.Context, client *Client, req *WSRequest) ([]byte, error) {
	// For now, use the same handler as PullMsg
	// In a full implementation, you'd pass the seq list to the service
	return s.HandlePullMsg(ctx, client, req)
}

// HandleGetConvMaxReadSeq handles get conversation max/read seq request
func (s *WsServer) HandleGetConvMaxReadSeq(ctx context.Context, client *Client, req *WSRequest) ([]byte, error) {
	var getSeqReq GetConvMaxReadSeqReq
	if err := json.Unmarshal(req.Data, &getSeqReq); err != nil {
		return nil, errcode.ErrInvalidParam
	}

	maxSeq, readSeq, err := s.convService.GetMaxReadSeq(ctx, client.UserId, getSeqReq.ConversationId)
	if err != nil {
		return nil, err
	}

	unreadCount := maxSeq - readSeq
	if unreadCount < 0 {
		unreadCount = 0
	}

	resp := GetConvMaxReadSeqResp{
		MaxSeq:      maxSeq,
		ReadSeq:     readSeq,
		UnreadCount: unreadCount,
	}

	return json.Marshal(resp)
}
