package cluster

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPublishToInstanceUsesInstanceChannel(t *testing.T) {
	_, rdb := newTestRedis(t)
	bus := NewRedisPushBus(rdb, PushSubscriptionConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	received := make(chan *PushEnvelope, 1)
	go func() {
		_ = bus.SubscribeInstance(ctx, "i1", func(_ context.Context, env *PushEnvelope) {
			received <- env
		}, nil, func(err error) {
			t.Errorf("subscribe runtime error: %v", err)
		})
	}()
	waitForCondition(t, time.Second, func() bool {
		return rdb.PubSubNumSub(context.Background(), pushChannelKey("i1")).Val()[pushChannelKey("i1")] > 0
	})

	env := &PushEnvelope{Version: 1, PushId: "p1", SourceInstance: "i0"}
	if err := bus.PublishToInstance(context.Background(), "i1", env); err != nil {
		t.Fatalf("publish to instance: %v", err)
	}

	select {
	case got := <-received:
		if got.PushId != "p1" {
			t.Fatalf("push id mismatch: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive published envelope")
	}
}

func TestSubscribeInstanceInvokesHandler(t *testing.T) {
	_, rdb := newTestRedis(t)
	bus := NewRedisPushBus(rdb, PushSubscriptionConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handled := make(chan struct{}, 1)
	go func() {
		_ = bus.SubscribeInstance(ctx, "i1", func(_ context.Context, env *PushEnvelope) {
			if env.PushId == "p1" {
				handled <- struct{}{}
			}
		}, nil, func(err error) {
			t.Errorf("subscribe runtime error: %v", err)
		})
	}()
	waitForCondition(t, time.Second, func() bool {
		return rdb.PubSubNumSub(context.Background(), pushChannelKey("i1")).Val()[pushChannelKey("i1")] > 0
	})

	if err := bus.PublishToInstance(context.Background(), "i1", &PushEnvelope{Version: 1, PushId: "p1"}); err != nil {
		t.Fatalf("publish to instance: %v", err)
	}

	select {
	case <-handled:
	case <-time.After(time.Second):
		t.Fatal("handler not invoked")
	}
}

func TestClosePreventsFurtherPublish(t *testing.T) {
	_, rdb := newTestRedis(t)
	bus := NewRedisPushBus(rdb, PushSubscriptionConfig{})

	if err := bus.Close(); err != nil {
		t.Fatalf("close push bus: %v", err)
	}
	if err := bus.PublishToInstance(context.Background(), "i1", &PushEnvelope{Version: 1}); err == nil {
		t.Fatal("expected publish after close to fail")
	}
}

func TestSubscribeInstanceReturnsErrorWhenChannelClosesUnexpectedly(t *testing.T) {
	_, rdb := newTestRedis(t)
	bus := NewRedisPushBus(rdb, PushSubscriptionConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runtimeErrs := make(chan error, 1)
	result := make(chan error, 1)
	go func() {
		result <- bus.SubscribeInstance(ctx, "i1", func(context.Context, *PushEnvelope) {}, nil, func(err error) {
			runtimeErrs <- err
		})
	}()

	waitForCondition(t, time.Second, func() bool {
		return rdb.PubSubNumSub(context.Background(), pushChannelKey("i1")).Val()[pushChannelKey("i1")] > 0
	})

	if err := rdb.Close(); err != nil {
		t.Fatalf("close redis client: %v", err)
	}

	select {
	case err := <-result:
		if !errors.Is(err, ErrPushSubscriptionClosed) {
			t.Fatalf("subscribe result mismatch: got %v want %v", err, ErrPushSubscriptionClosed)
		}
	case <-time.After(time.Second):
		t.Fatal("expected subscribe to return an error on unexpected close")
	}

	select {
	case err := <-runtimeErrs:
		if !errors.Is(err, ErrPushSubscriptionClosed) {
			t.Fatalf("runtime error mismatch: got %v want %v", err, ErrPushSubscriptionClosed)
		}
	default:
	}
}

func TestSubscribeInstanceInvokesReadyCallback(t *testing.T) {
	_, rdb := newTestRedis(t)
	bus := NewRedisPushBus(rdb, PushSubscriptionConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{}, 1)
	go func() {
		_ = bus.SubscribeInstance(ctx, "i1", func(context.Context, *PushEnvelope) {}, func() {
			ready <- struct{}{}
		}, nil)
	}()

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("ready callback not invoked")
	}
}

func TestSubscribeInstanceUsesConfiguredChannelBufferAndSendTimeout(t *testing.T) {
	_, rdb := newTestRedis(t)
	bus := NewRedisPushBus(rdb, PushSubscriptionConfig{
		ChannelSize: 1,
		SendTimeout: 20 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	release := make(chan struct{})
	received := make(chan string, 3)
	go func() {
		_ = bus.SubscribeInstance(ctx, "i1", func(_ context.Context, env *PushEnvelope) {
			received <- env.PushId
			if env.PushId == "p1" {
				<-release
			}
		}, nil, func(err error) {
			t.Errorf("subscribe runtime error: %v", err)
		})
	}()
	waitForCondition(t, time.Second, func() bool {
		return rdb.PubSubNumSub(context.Background(), pushChannelKey("i1")).Val()[pushChannelKey("i1")] > 0
	})

	for _, pushID := range []string{"p1", "p2", "p3"} {
		if err := bus.PublishToInstance(context.Background(), "i1", &PushEnvelope{Version: 1, PushId: pushID}); err != nil {
			t.Fatalf("publish to instance: %v", err)
		}
	}

	time.Sleep(100 * time.Millisecond)
	close(release)

	var got []string
	waitForCondition(t, time.Second, func() bool {
		for len(received) > 0 {
			got = append(got, <-received)
		}
		return len(got) >= 2
	})

	if len(got) != 2 {
		t.Fatalf("received push count mismatch: got %d want 2", len(got))
	}
}
