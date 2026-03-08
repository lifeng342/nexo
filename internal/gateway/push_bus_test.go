package gateway

import (
	"context"
	"testing"
)

func TestRedisPushBusClosePreventsNewSubscriptions(t *testing.T) {
	bus := NewRedisPushBus(nil)
	if err := bus.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	handler := func(context.Context, *PushEnvelope) {}
	onRuntimeError := func(error) {}

	if err := bus.SubscribeInstance(context.Background(), "inst-a", handler, onRuntimeError); err == nil {
		t.Fatal("SubscribeInstance() after Close() error = nil, want non-nil")
	}
	if err := bus.SubscribeBroadcast(context.Background(), handler, onRuntimeError); err == nil {
		t.Fatal("SubscribeBroadcast() after Close() error = nil, want non-nil")
	}
}
