package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestReconcileRepairsMissingRouteEntry(t *testing.T) {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	defer rdb.Close()
	_ = rdb.FlushDB(ctx).Err()

	store := NewRouteStore(rdb, time.Minute)
	require.NoError(t, store.ReconcileInstanceRoutes(ctx, "inst-a", []RouteConn{{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}}, 100))
	got, err := rdb.HGet(ctx, routeUserKey("u1"), "c1").Result()
	require.NoError(t, err)
	require.Equal(t, "inst-a|1", got)
}

func TestReconcileRemovesStaleInstanceOwnedConn(t *testing.T) {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	defer rdb.Close()
	_ = rdb.FlushDB(ctx).Err()

	store := NewRouteStore(rdb, time.Minute)
	conn := RouteConn{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}
	require.NoError(t, store.RegisterConn(ctx, conn))
	require.NoError(t, store.ReconcileInstanceRoutes(ctx, "inst-a", nil, 100))
	exists, err := rdb.HExists(ctx, routeUserKey("u1"), "c1").Result()
	require.NoError(t, err)
	require.False(t, exists)
}

func TestReconcileRespectsStaleCleanupLimit(t *testing.T) {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	defer rdb.Close()
	_ = rdb.FlushDB(ctx).Err()

	store := NewRouteStore(rdb, time.Minute)
	require.NoError(t, store.RegisterConn(ctx, RouteConn{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}))
	require.NoError(t, store.RegisterConn(ctx, RouteConn{UserId: "u2", ConnId: "c2", InstanceId: "inst-a", PlatformId: 1}))

	require.NoError(t, store.ReconcileInstanceRoutes(ctx, "inst-a", nil, 1))
	remaining, err := rdb.SMembers(ctx, routeInstKey("inst-a")).Result()
	require.NoError(t, err)
	require.Len(t, remaining, 1)
}

func TestHandleRouteWriteEventClearsConvergedRegisteredStateWithRouteStore(t *testing.T) {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	defer rdb.Close()
	_ = rdb.FlushDB(ctx).Err()

	server := newRouteRuntimeTestServer()
	server.routeStore = NewRouteStore(rdb, time.Minute)
	client := &Client{UserId: "u1", ConnId: "c1", PlatformId: 1}
	server.userMap.Register(ctx, client)
	desc := routeConnDescriptor{UserID: "u1", ConnID: "c1", PlatformID: 1, InstanceID: "inst-a"}
	server.markRouteConnDesired(desc, routeDesiredRegistered)

	server.handleRouteWriteEvent(ctx, routeWriteEvent{Descriptor: desc, Desired: routeDesiredRegistered})

	_, ok := server.getRouteConnState(routeConnKey{UserID: "u1", ConnID: "c1"})
	require.False(t, ok)
}
