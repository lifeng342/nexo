package gateway

import (
	"context"
	"fmt"
	"testing"

	"github.com/mbeoliero/nexo/pkg/constant"
)

func TestRouteStoreDoesNotCleanupRouteWhenAliveTemporarilyMissing(t *testing.T) {
	ctx := context.Background()
	store, rdb, cleanup := newTestRouteStore(t)
	defer cleanup()

	conn := RouteConn{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}
	if err := store.RegisterConn(ctx, conn); err != nil {
		t.Fatalf("RegisterConn() error = %v", err)
	}

	routes, err := store.GetUsersConnRefs(ctx, []string{"u1"})
	if err != nil {
		t.Fatalf("GetUsersConnRefs() error = %v", err)
	}
	if got := len(routes["u1"]); got != 0 {
		t.Fatalf("GetUsersConnRefs() len = %d, want 0 when instance is not alive", got)
	}

	userKey := fmt.Sprintf(constant.RedisKeyRouteUser(), "u1")
	if exists := rdb.HExists(ctx, userKey, "c1").Val(); !exists {
		t.Fatal("route field was cleaned up while instance index still existed")
	}
}
