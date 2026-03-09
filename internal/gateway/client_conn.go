package gateway

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mbeoliero/kit/log"
)

// ClientConn represents a WebSocket connection wrapper
type ClientConn interface {
	ReadMessage() ([]byte, error)
	WriteMessage(data []byte) error
	WriteControlMessage(data []byte) error
	Close() error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
}

// WebsocketClientConn implements ClientConn using gorilla/websocket
type WebsocketClientConn struct {
	conn        *websocket.Conn
	writeChan   chan []byte
	controlChan chan controlWrite
	writeMu     sync.Mutex
	closeOnce   sync.Once
	closed      bool
	closeChan   chan struct{}
	pingPeriod  time.Duration
	pongWait    time.Duration
	writeWait   time.Duration
	maxMsgSize  int64
}

 type controlWrite struct {
	data []byte
	ack  chan error
}

// NewWebSocketClientConn creates a new websocket client connection
func NewWebSocketClientConn(conn *websocket.Conn, maxMsgSize int64, writeWait, pongWait, pingPeriod time.Duration, writeChannelSize int) *WebsocketClientConn {
	if writeChannelSize <= 0 {
		writeChannelSize = 256
	}
	c := &WebsocketClientConn{
		conn:        conn,
		writeChan:   make(chan []byte, writeChannelSize),
		controlChan: make(chan controlWrite),
		closeChan:   make(chan struct{}),
		pingPeriod:  pingPeriod,
		pongWait:    pongWait,
		writeWait:   writeWait,
		maxMsgSize:  maxMsgSize,
	}

	if conn != nil {
		conn.SetReadLimit(maxMsgSize)
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})
		go c.writeLoop()
	}

	return c
}

// writeLoop handles all writes to the connection (single writer pattern)
func (c *WebsocketClientConn) writeLoop() {
	ticker := time.NewTicker(c.pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case ctrl := <-c.controlChan:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeWait))
			err := c.conn.WriteMessage(websocket.BinaryMessage, ctrl.data)
			ctrl.ack <- err
			close(ctrl.ack)
			if err != nil {
				log.Warn("write control message error: %v", err)
				return
			}
		case message, ok := <-c.writeChan:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeWait))
			if !ok {
				// Channel closed, send close message
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.BinaryMessage, message); err != nil {
				log.Warn("write message error: %v", err)
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Debug("ping error: %v", err)
				return
			}

		case <-c.closeChan:
			return
		}
	}
}

// ReadMessage reads a message from the connection
func (c *WebsocketClientConn) ReadMessage() ([]byte, error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(c.pongWait))
	_, message, err := c.conn.ReadMessage()
	return message, err
}

// WriteMessage queues a message to be written
func (c *WebsocketClientConn) WriteMessage(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closed {
		return ErrConnClosed
	}

	select {
	case c.writeChan <- data:
		return nil
	default:
		// Channel full, connection is slow consumer
		return ErrWriteChannelFull
	}
}

func (c *WebsocketClientConn) WriteControlMessage(data []byte) error {
	c.writeMu.Lock()
	if c.closed {
		c.writeMu.Unlock()
		return ErrConnClosed
	}
	if c.conn == nil {
		c.writeMu.Unlock()
		return nil
	}
	ack := make(chan error, 1)
	ctrl := controlWrite{data: data, ack: ack}
	c.writeMu.Unlock()

	select {
	case c.controlChan <- ctrl:
		return <-ack
	case <-c.closeChan:
		return ErrConnClosed
	}
}

// Close closes the connection
func (c *WebsocketClientConn) Close() error {
	c.closeOnce.Do(func() {
		c.writeMu.Lock()
		c.closed = true
		close(c.writeChan)
		c.writeMu.Unlock()

		close(c.closeChan)
	})
	return nil
}

// SetReadDeadline sets the read deadline
func (c *WebsocketClientConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline
func (c *WebsocketClientConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}
