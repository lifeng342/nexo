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
	require.NoError(t, store.ReconcileInstanceRoutes(ctx, "inst-a", []RouteConn{{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}}))
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
	require.NoError(t, store.ReconcileInstanceRoutes(ctx, "inst-a", nil))
	exists, err := rdb.HExists(ctx, routeUserKey("u1"), "c1").Result()
	require.NoError(t, err)
	require.False(t, exists)
}
