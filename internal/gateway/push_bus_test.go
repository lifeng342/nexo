package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/mbeoliero/nexo/pkg/constant"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func redisClientForTest(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 9})
	t.Cleanup(func() {
		_ = rdb.Close()
	})
	_ = rdb.FlushDB(context.Background()).Err()
	return rdb
}

func TestPublishToInstanceUsesInstanceChannel(t *testing.T) {
	constant.InitRedisKeyPrefix("test:")
	bus := NewInMemoryPushBus()
	ctx := context.Background()
	got := make(chan *PushEnvelope, 1)
	require.NoError(t, bus.SubscribeInstance(ctx, "inst-a", func(ctx context.Context, env *PushEnvelope) { got <- env }, func(error) {}))
	env := &PushEnvelope{PushId: "p1", Mode: PushModeRoute, TargetConnMap: map[string][]ConnRef{"inst-a": {{UserId: "u1", ConnId: "c1", PlatformId: 1}}}}
	require.NoError(t, bus.PublishToInstance(ctx, "inst-a", env))

	select {
	case received := <-got:
		require.Equal(t, "p1", received.PushId)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for envelope")
	}
}

func TestSubscribeInstanceInvokesHandler(t *testing.T) {
	bus := NewInMemoryPushBus()
	ctx := context.Background()
	called := make(chan struct{}, 1)
	require.NoError(t, bus.SubscribeInstance(ctx, "inst-a", func(ctx context.Context, env *PushEnvelope) { called <- struct{}{} }, func(error) {}))
	require.NoError(t, bus.PublishToInstance(ctx, "inst-a", &PushEnvelope{PushId: "p1", Mode: PushModeRoute}))

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("handler not called")
	}
}

func TestClosePreventsFurtherPublish(t *testing.T) {
	bus := NewInMemoryPushBus()
	require.NoError(t, bus.Close())
	require.Error(t, bus.PublishToInstance(context.Background(), "inst-a", &PushEnvelope{}))
}

func TestSubscribeInstanceBadPayloadIsNonFatal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	constant.InitRedisKeyPrefix("test:")

	rdb := redisClientForTest(t)
	bus := NewRedisPushBus(rdb)
	called := make(chan struct{}, 1)
	runtimeErrs := make(chan error, 1)
	require.NoError(t, bus.SubscribeInstance(ctx, "inst-a", func(ctx context.Context, env *PushEnvelope) {
		called <- struct{}{}
	}, func(err error) {
		runtimeErrs <- err
	}))

	require.NoError(t, rdb.Publish(ctx, pushBusChannel("inst-a"), "not-json").Err())
	select {
	case err := <-runtimeErrs:
		t.Fatalf("unexpected runtime error: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	require.NoError(t, bus.PublishToInstance(ctx, "inst-a", &PushEnvelope{PushId: "p2", Mode: PushModeRoute}))
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("handler not called after bad payload")
	}
}
