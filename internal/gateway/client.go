package gateway

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/mbeoliero/kit/log"
)

// Client represents a connected WebSocket client
type Client struct {
	mu         sync.Mutex
	conn       ClientConn
	UserId     string
	PlatformId int
	SDKType    string
	Token      string
	ConnId     string
	server     *WsServer
	closed     atomic.Bool
	closedErr  error
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewClient creates a new client
func NewClient(conn ClientConn, userId string, platformId int, sdkType, token, connId string, server *WsServer) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		conn:       conn,
		UserId:     userId,
		PlatformId: platformId,
		SDKType:    sdkType,
		Token:      token,
		ConnId:     connId,
		server:     server,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start starts the client message handling
func (c *Client) Start() {
	go c.readLoop()
}

// readLoop continuously reads messages from the connection
func (c *Client) readLoop() {
	defer func() {
		if r := recover(); r != nil {
			c.closedErr = ErrPanic
			log.CtxError(c.ctx, "client read loop panic: user_id=%s, error=%v", c.UserId, r)
		}
		c.close()
	}()

	for {
		message, err := c.conn.ReadMessage()
		if err != nil {
			log.CtxDebug(c.ctx, "read message error: user_id=%s, error=%v", c.UserId, err)
			c.closedErr = err
			return
		}

		if c.closed.Load() {
			c.closedErr = ErrConnClosed
			return
		}

		if err := c.handleMessage(message); err != nil {
			log.CtxWarn(c.ctx, "handle message error: user_id=%s, error=%v", c.UserId, err)
			c.closedErr = err
			return
		}
	}
}

// handleMessage handles a single incoming message
func (c *Client) handleMessage(message []byte) error {
	var req WSRequest
	if err := json.Unmarshal(message, &req); err != nil {
		return c.replyError(&req, ErrInvalidProtocol)
	}

	// Validate sender Id matches authenticated user
	if req.SendId != "" && req.SendId != c.UserId {
		return c.replyError(&req, ErrUserIdMismatch)
	}

	log.CtxDebug(c.ctx, "received message: req_identifier=%d, user_id=%s", req.ReqIdentifier, c.UserId)

	var resp []byte
	var err error

	switch req.ReqIdentifier {
	case WSGetNewestSeq:
		resp, err = c.server.HandleGetNewestSeq(c.ctx, c, &req)
	case WSSendMsg:
		resp, err = c.server.HandleSendMsg(c.ctx, c, &req)
	case WSPullMsgBySeqList:
		resp, err = c.server.HandlePullMsgBySeqList(c.ctx, c, &req)
	case WSPullMsg:
		resp, err = c.server.HandlePullMsg(c.ctx, c, &req)
	case WSGetConvMaxReadSeq:
		resp, err = c.server.HandleGetConvMaxReadSeq(c.ctx, c, &req)
	default:
		return c.replyError(&req, ErrInvalidProtocol)
	}

	return c.reply(&req, err, resp)
}

// reply sends a response to the client
func (c *Client) reply(req *WSRequest, err error, data []byte) error {
	resp := WSResponse{
		ReqIdentifier: req.ReqIdentifier,
		MsgIncr:       req.MsgIncr,
		OperationId:   req.OperationId,
		Data:          data,
	}

	if err != nil {
		resp.ErrCode = 1
		resp.ErrMsg = err.Error()
	}

	return c.writeResponse(resp)
}

// replyError sends an error response
func (c *Client) replyError(req *WSRequest, err error) error {
	resp := WSResponse{
		ReqIdentifier: req.ReqIdentifier,
		MsgIncr:       req.MsgIncr,
		OperationId:   req.OperationId,
		ErrCode:       1,
		ErrMsg:        err.Error(),
	}
	return c.writeResponse(resp)
}

// writeResponse writes a response to the connection
func (c *Client) writeResponse(resp WSResponse) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed.Load() {
		return nil
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	return c.conn.WriteMessage(data)
}

// PushMessage pushes a message to the client
func (c *Client) PushMessage(ctx context.Context, msg *MessageData) error {
	if c.closed.Load() {
		return ErrConnClosed
	}

	pushData := &PushMsgData{
		Msgs: map[string][]*MessageData{
			msg.ConversationId: {msg},
		},
	}

	data, err := json.Marshal(pushData)
	if err != nil {
		return err
	}

	resp := WSResponse{
		ReqIdentifier: WSPushMsg,
		Data:          data,
	}

	return c.writeResponse(resp)
}

// KickOnline sends kick message and closes connection
func (c *Client) KickOnline() error {
	resp := WSResponse{
		ReqIdentifier: WSKickOnlineMsg,
	}
	c.writeResponse(resp)
	return c.Close()
}

// Close closes the client connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed.Load() {
		return nil
	}

	c.closed.Store(true)
	c.cancel()
	return c.conn.Close()
}

// close handles cleanup when connection is closed
func (c *Client) close() {
	c.Close()
	c.server.UnregisterClient(c)
}

// IsClosed returns whether the client is closed
func (c *Client) IsClosed() bool {
	return c.closed.Load()
}
