package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewWebSocketClientConnUsesConfiguredWriteChannelSize(t *testing.T) {
	conn := NewWebSocketClientConn(nil, 1024, time.Second, time.Second, time.Second, 8)
	require.Equal(t, 8, cap(conn.writeChan))
}

func TestWriteMessageReturnsErrWriteChannelFullWhenBufferIsFull(t *testing.T) {
	conn := &WebsocketClientConn{writeChan: make(chan []byte, 1)}
	require.NoError(t, conn.WriteMessage([]byte("a")))
	require.ErrorIs(t, conn.WriteMessage([]byte("b")), ErrWriteChannelFull)
}

func TestWriteControlMessageBypassesQueue(t *testing.T) {
	conn := &WebsocketClientConn{writeChan: make(chan []byte, 1)}
	require.NoError(t, conn.WriteMessage([]byte("a")))
	require.NoError(t, conn.WriteControlMessage([]byte("kick")))
}
