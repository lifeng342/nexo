package gateway

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/pkg/constant"
)

func TestRouteStoreFiltersPushRouteAndPresenceSeparately(t *testing.T) {
	ctx := context.Background()
	store, rdb, cleanup := newTestRouteStore(t)
	defer cleanup()

	connA := RouteConn{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}
	connB := RouteConn{UserId: "u1", ConnId: "c2", InstanceId: "inst-b", PlatformId: 2}

	if err := store.RegisterConn(ctx, connA); err != nil {
		t.Fatalf("RegisterConn(connA) error = %v", err)
	}
	if err := store.RegisterConn(ctx, connB); err != nil {
		t.Fatalf("RegisterConn(connB) error = %v", err)
	}

	mustSetInstanceState(t, ctx, rdb, "inst-a", true, true, true, false)
	mustSetInstanceState(t, ctx, rdb, "inst-b", true, false, true, true)

	pushRoutes, err := store.GetUsersConnRefs(ctx, []string{"u1"})
	if err != nil {
		t.Fatalf("GetUsersConnRefs() error = %v", err)
	}
	if got := pushRoutes["u1"]; len(got) != 1 || got[0].ConnId != "c1" {
		t.Fatalf("GetUsersConnRefs() = %+v, want only connA", got)
	}

	presenceRoutes, err := store.GetUsersPresenceConnRefs(ctx, []string{"u1"})
	if err != nil {
		t.Fatalf("GetUsersPresenceConnRefs() error = %v", err)
	}
	if got := presenceRoutes["u1"]; len(got) != 2 {
		t.Fatalf("GetUsersPresenceConnRefs() len = %d, want 2", len(got))
	}
}

func TestRouteStoreReconcileInstanceRoutes(t *testing.T) {
	ctx := context.Background()
	store, rdb, cleanup := newTestRouteStore(t)
	defer cleanup()

	connKeep := RouteConn{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}
	connDrop := RouteConn{UserId: "u2", ConnId: "c2", InstanceId: "inst-a", PlatformId: 2}

	if err := store.RegisterConn(ctx, connKeep); err != nil {
		t.Fatalf("RegisterConn(connKeep) error = %v", err)
	}
	if err := store.RegisterConn(ctx, connDrop); err != nil {
		t.Fatalf("RegisterConn(connDrop) error = %v", err)
	}

	if err := store.ReconcileInstanceRoutes(ctx, "inst-a", []RouteConn{connKeep}); err != nil {
		t.Fatalf("ReconcileInstanceRoutes() error = %v", err)
	}

	mustSetInstanceState(t, ctx, rdb, "inst-a", true, true, true, false)

	pushRoutes, err := store.GetUsersConnRefs(ctx, []string{"u1", "u2"})
	if err != nil {
		t.Fatalf("GetUsersConnRefs() error = %v", err)
	}
	if got := len(pushRoutes["u1"]); got != 1 {
		t.Fatalf("GetUsersConnRefs()[u1] len = %d, want 1", got)
	}
	if got := len(pushRoutes["u2"]); got != 0 {
		t.Fatalf("GetUsersConnRefs()[u2] len = %d, want 0", got)
	}

	userKey := fmt.Sprintf(constant.RedisKeyRouteUser(), "u2")
	if exists := rdb.HExists(ctx, userKey, "c2").Val(); exists {
		t.Fatal("stale user route still exists after reconcile")
	}
	instKey := fmt.Sprintf(constant.RedisKeyRouteInstance(), "inst-a")
	if members := rdb.SMembers(ctx, instKey).Val(); len(members) != 1 || members[0] != "u1|c1" {
		t.Fatalf("instance route index = %v, want [u1|c1]", members)
	}
}

func newTestRouteStore(t *testing.T) (*RouteStore, *redis.Client, func()) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := &config.Config{
		WebSocket: config.WebSocketConfig{
			RouteStaleCleanupLimit:         64,
			RouteTTLRefreshIntervalSeconds: 20,
			RouteReconcileIntervalSeconds:  60,
			CrossInstance: config.CrossInstanceConfig{
				RouteTTLSeconds:         70,
				InstanceAliveTTLSeconds: 30,
			},
		},
	}

	oldPrefix := constant.GetRedisKeyPrefix()
	constant.InitRedisKeyPrefix("nexo:test:")
	store := NewRouteStore(rdb, cfg)

	cleanup := func() {
		_ = rdb.Close()
		mr.Close()
		constant.InitRedisKeyPrefix(oldPrefix)
	}

	return store, rdb, cleanup
}

func mustSetInstanceState(t *testing.T, ctx context.Context, rdb *redis.Client, instanceId string, alive, routeable, broadcastReady, draining bool) {
	t.Helper()

	key := fmt.Sprintf(constant.RedisKeyInstanceAlive(), instanceId)
	if err := rdb.HSet(ctx, key, map[string]any{
		"instance_id":     instanceId,
		"ready":           boolToRedis(routeable),
		"routeable":       boolToRedis(routeable),
		"broadcast_ready": boolToRedis(broadcastReady),
		"draining":        boolToRedis(draining),
		"started_at":      time.Now().UnixMilli(),
		"last_heartbeat":  time.Now().UnixMilli(),
		"conn_count":      1,
	}).Err(); err != nil {
		t.Fatalf("HSet(%s) error = %v", key, err)
	}
	if alive {
		if err := rdb.Expire(ctx, key, 30*time.Second).Err(); err != nil {
			t.Fatalf("Expire(%s) error = %v", key, err)
		}
		return
	}
	if err := rdb.Del(ctx, key).Err(); err != nil {
		t.Fatalf("Del(%s) error = %v", key, err)
	}
}
