package gateway

import (
	"sync"
	"time"

	"github.com/hertz-contrib/websocket"
	"github.com/mbeoliero/kit/log"
)

// hertzWebSocketClientConn implements ClientConn using hertz-contrib/websocket
type hertzWebSocketClientConn struct {
	conn        *websocket.Conn
	writeChan   chan []byte
	writeMu     sync.Mutex
	closeOnce   sync.Once
	closed      bool
	closeChan   chan struct{}
	pingPeriod  time.Duration
	pongWait    time.Duration
	writeWait   time.Duration
	maxMsgSize  int64
}

// NewHertzWebSocketClientConn creates a new hertz websocket client connection
func NewHertzWebSocketClientConn(conn *websocket.Conn, maxMsgSize int64, pongWait, pingPeriod time.Duration) *hertzWebSocketClientConn {
	c := &hertzWebSocketClientConn{
		conn:       conn,
		writeChan:  make(chan []byte, 256), // Buffered write channel
		closeChan:  make(chan struct{}),
		pingPeriod: pingPeriod,
		pongWait:   pongWait,
		writeWait:  WriteWait,
		maxMsgSize: maxMsgSize,
	}

	// Set read limit
	conn.SetReadLimit(maxMsgSize)

	// Set pong handler to extend read deadline
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Start write loop
	go c.writeLoop()

	return c
}

// writeLoop handles all writes to the connection (single writer pattern)
func (c *hertzWebSocketClientConn) writeLoop() {
	ticker := time.NewTicker(c.pingPeriod)
	defer func() {
		ticker.Stop()
		// Recover from panic when writing to closed connection
		if r := recover(); r != nil {
			log.Debug("writeLoop recovered from panic: %v", r)
		}
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.writeChan:
			if !ok {
				// Channel closed, try to send close message but don't panic
				c.safeWriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.safeWriteMessage(websocket.BinaryMessage, message); err != nil {
				log.Debug("write message error: %v", err)
				return
			}

		case <-ticker.C:
			if err := c.safeWriteMessage(websocket.PingMessage, nil); err != nil {
				log.Debug("ping error: %v", err)
				return
			}

		case <-c.closeChan:
			return
		}
	}
}

// safeWriteMessage writes a message with proper error handling
func (c *hertzWebSocketClientConn) safeWriteMessage(messageType int, data []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Debug("safeWriteMessage recovered from panic: %v", r)
			err = ErrConnClosed
		}
	}()

	c.conn.SetWriteDeadline(time.Now().Add(c.writeWait))
	return c.conn.WriteMessage(messageType, data)
}

// ReadMessage reads a message from the connection
func (c *hertzWebSocketClientConn) ReadMessage() ([]byte, error) {
	c.conn.SetReadDeadline(time.Now().Add(c.pongWait))
	_, message, err := c.conn.ReadMessage()
	return message, err
}

// WriteMessage queues a message to be written
func (c *hertzWebSocketClientConn) WriteMessage(data []byte) error {
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

// Close closes the connection
func (c *hertzWebSocketClientConn) Close() error {
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
func (c *hertzWebSocketClientConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline
func (c *hertzWebSocketClientConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}
