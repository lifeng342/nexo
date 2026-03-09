package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/mbeoliero/nexo/pkg/constant"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRegisterConnWritesUserRouteAndInstanceIndex(t *testing.T) {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	defer rdb.Close()
	_ = rdb.FlushDB(ctx).Err()
	constant.InitRedisKeyPrefix("test:")

	store := NewRouteStore(rdb, time.Minute)
	conn := RouteConn{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}
	require.NoError(t, store.RegisterConn(ctx, conn))

	got, err := rdb.HGet(ctx, routeUserKey("u1"), "c1").Result()
	require.NoError(t, err)
	require.Equal(t, "inst-a|1", got)
	isMember, err := rdb.SIsMember(ctx, routeInstKey("inst-a"), "u1|c1").Result()
	require.NoError(t, err)
	require.True(t, isMember)
	require.Equal(t, "test:route:user:u1", routeUserKey("u1"))
	require.Equal(t, "test:route:inst:inst-a", routeInstKey("inst-a"))
}

func TestGetUsersConnRefsFiltersByAliveAndRouteable(t *testing.T) {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	defer rdb.Close()
	_ = rdb.FlushDB(ctx).Err()

	store := NewRouteStore(rdb, time.Minute)
	require.NoError(t, store.RegisterConn(ctx, RouteConn{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}))
	require.NoError(t, writeInstanceAlive(ctx, rdb, "inst-a", true, true, false))

	refs, err := store.GetUsersConnRefs(ctx, []string{"u1"})
	require.NoError(t, err)
	require.Len(t, refs["u1"], 1)

	require.NoError(t, writeInstanceAlive(ctx, rdb, "inst-a", false, false, true))
	refs, err = store.GetUsersConnRefs(ctx, []string{"u1"})
	require.NoError(t, err)
	require.Len(t, refs["u1"], 0)
}

func TestGetUsersPresenceConnRefsFiltersOnlyByAlive(t *testing.T) {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	defer rdb.Close()
	_ = rdb.FlushDB(ctx).Err()

	store := NewRouteStore(rdb, time.Minute)
	require.NoError(t, store.RegisterConn(ctx, RouteConn{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}))
	require.NoError(t, writeInstanceAlive(ctx, rdb, "inst-a", false, false, true))

	refs, err := store.GetUsersPresenceConnRefs(ctx, []string{"u1"})
	require.NoError(t, err)
	require.Len(t, refs["u1"], 1)
}

func TestUnregisterConnRemovesRouteAndIndex(t *testing.T) {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	defer rdb.Close()
	_ = rdb.FlushDB(ctx).Err()

	store := NewRouteStore(rdb, time.Minute)
	conn := RouteConn{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}
	require.NoError(t, store.RegisterConn(ctx, conn))
	require.NoError(t, store.UnregisterConn(ctx, conn))

	exists, err := rdb.HExists(ctx, routeUserKey("u1"), "c1").Result()
	require.NoError(t, err)
	require.False(t, exists)
	isMember, err := rdb.SIsMember(ctx, routeInstKey("inst-a"), "u1|c1").Result()
	require.NoError(t, err)
	require.False(t, isMember)
}
