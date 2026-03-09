package gateway

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisterDoesNotRequireRedisSideEffect(t *testing.T) {
	m := NewUserMap(nil)
	client := &Client{UserId: "u1", ConnId: "c1", PlatformId: 1}
	m.Register(context.Background(), client)
	clients, ok := m.GetAll("u1")
	require.True(t, ok)
	require.Len(t, clients, 1)
}

func TestSnapshotRouteConnsReturnsAllLocalConnections(t *testing.T) {
	m := NewUserMap(nil)
	m.Register(context.Background(), &Client{UserId: "u1", ConnId: "c1", PlatformId: 1})
	m.Register(context.Background(), &Client{UserId: "u1", ConnId: "c2", PlatformId: 2})
	got := m.SnapshotRouteConns("inst-a")
	require.ElementsMatch(t, []RouteConn{
		{UserId: "u1", ConnId: "c1", PlatformId: 1, InstanceId: "inst-a"},
		{UserId: "u1", ConnId: "c2", PlatformId: 2, InstanceId: "inst-a"},
	}, got)
}

func TestUnregisterReturnsRemovedAndUserOffline(t *testing.T) {
	m := NewUserMap(nil)
	m.Register(context.Background(), &Client{UserId: "u1", ConnId: "c1", PlatformId: 1})
	m.Register(context.Background(), &Client{UserId: "u1", ConnId: "c2", PlatformId: 2})

	removed, offline := m.Unregister(context.Background(), &Client{UserId: "u1", ConnId: "missing"})
	require.False(t, removed)
	require.False(t, offline)

	removed, offline = m.Unregister(context.Background(), &Client{UserId: "u1", ConnId: "c1"})
	require.True(t, removed)
	require.False(t, offline)

	removed, offline = m.Unregister(context.Background(), &Client{UserId: "u1", ConnId: "c2"})
	require.True(t, removed)
	require.True(t, offline)
}
