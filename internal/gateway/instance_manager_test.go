package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestInstanceManagerWritesReadyRouteableAndDraining(t *testing.T) {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	defer rdb.Close()
	_ = rdb.FlushDB(ctx).Err()

	mgr := NewInstanceManager(rdb, "inst-a", time.Minute)
	require.NoError(t, mgr.UpdateState(ctx, InstanceState{InstanceID: "inst-a", Ready: true, Routeable: true, Draining: false}))

	state, err := mgr.GetState(ctx, "inst-a")
	require.NoError(t, err)
	require.True(t, state.Ready)
	require.True(t, state.Routeable)
	require.False(t, state.Draining)
}

func TestInstanceManagerReadyAndRouteableCanDiverge(t *testing.T) {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	defer rdb.Close()
	_ = rdb.FlushDB(ctx).Err()

	mgr := NewInstanceManager(rdb, "inst-a", time.Minute)
	require.NoError(t, mgr.UpdateState(ctx, InstanceState{InstanceID: "inst-a", Ready: false, Routeable: true, Draining: true}))

	state, err := mgr.GetState(ctx, "inst-a")
	require.NoError(t, err)
	require.False(t, state.Ready)
	require.True(t, state.Routeable)
	require.True(t, state.Draining)
}

func TestInstanceManagerStartRefreshesHeartbeat(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	defer rdb.Close()
	_ = rdb.FlushDB(ctx).Err()

	mgr := NewInstanceManager(rdb, "inst-a", time.Second)
	gate := NewLifecycleGate()
	mgr.Start(ctx, gate, 50*time.Millisecond)
	time.Sleep(120 * time.Millisecond)

	state, err := mgr.GetState(context.Background(), "inst-a")
	require.NoError(t, err)
	require.True(t, state.Ready)
	require.True(t, state.Routeable)
}
