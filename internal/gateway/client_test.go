package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeClientConn struct {
	writes        [][]byte
	controlWrites [][]byte
	closed        bool
}

func (f *fakeClientConn) ReadMessage() ([]byte, error) { return nil, nil }
func (f *fakeClientConn) WriteMessage(data []byte) error {
	f.writes = append(f.writes, data)
	return nil
}
func (f *fakeClientConn) WriteControlMessage(data []byte) error {
	f.controlWrites = append(f.controlWrites, data)
	return nil
}
func (f *fakeClientConn) Close() error { f.closed = true; return nil }
func (f *fakeClientConn) SetReadDeadline(t time.Time) error { return nil }
func (f *fakeClientConn) SetWriteDeadline(t time.Time) error { return nil }

func TestKickOnlinePrefersControlWriteBeforeClose(t *testing.T) {
	conn := &fakeClientConn{}
	client := NewClient(conn, "u1", 1, "sdk", "token", "c1", &WsServer{})

	err := client.KickOnline()

	require.NoError(t, err)
	require.Len(t, conn.controlWrites, 1)
	require.Len(t, conn.writes, 0)
	require.True(t, conn.closed)
}
