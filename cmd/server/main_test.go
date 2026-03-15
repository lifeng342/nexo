package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/internal/gateway"
	"github.com/mbeoliero/nexo/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestBuildServerDependenciesGeneratesInstanceIDWhenEmpty(t *testing.T) {
	cfg := &config.Config{}
	cfg.WebSocket.CrossInstance.InstanceID = ""
	deps, err := buildServerDependencies(cfg, nil)
	require.NoError(t, err)
	require.NotEmpty(t, deps.InstanceID)
}

func TestBuildServerDependenciesWiresMessageHandlerWithGate(t *testing.T) {
	cfg := &config.Config{}
	deps, err := buildServerDependencies(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, deps.Gate)
	require.NotNil(t, deps.MessageHandler)
}

func TestBuildServerDependenciesWiresUserHandlerWithPresenceService(t *testing.T) {
	cfg := &config.Config{}
	deps, err := buildServerDependencies(cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, deps.Presence)
	require.NotNil(t, deps.UserHandler)
}

func newTestRepositoriesForWiring(t *testing.T) *repository.Repositories {
	t.Helper()
	return &repository.Repositories{}
}

func TestBuildServerDependenciesEnablesCrossInstanceWiring(t *testing.T) {
	cfg := &config.Config{}
	cfg.WebSocket.CrossInstance.Enabled = true
	cfg.WebSocket.CrossInstance.PushEnvelopeSecret = "secret"
	cfg.Server.HTTPPort = 8080
	repos := newTestRepositoriesForWiring(t)
	deps, err := buildServerDependencies(cfg, repos)
	require.NoError(t, err)
	require.NotNil(t, deps.RouteStore)
	require.NotNil(t, deps.InstanceManager)
	require.NotNil(t, deps.PushBus)
	require.NotNil(t, deps.PushCoordinator)
}

func TestBuildServerDependenciesRequiresPushEnvelopeSecretWhenCrossInstanceEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.WebSocket.CrossInstance.Enabled = true
	cfg.Server.HTTPPort = 8080
	repos := newTestRepositoriesForWiring(t)
	_, err := buildServerDependencies(cfg, repos)
	require.Error(t, err)
}

func TestWaitForSendDrainIgnoresSeparateSignalContextCancellation(t *testing.T) {
	gate := gateway.NewLifecycleGate()
	release, err := gate.AcquireSendLease()
	require.NoError(t, err)

	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	defer runtimeCancel()
	_, signalCancel := context.WithCancel(context.Background())
	signalCancel()

	done := make(chan struct{})
	go func() {
		waitForSendDrain(runtimeCtx, gate)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("waitForSendDrain returned before inflight lease drained")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitForSendDrain did not finish after release")
	}

}

func TestWaitForPushDrainBlocksUntilPendingPushesFinish(t *testing.T) {
	server := &gateway.WsServer{}
	server.MarkPushInFlightForTest(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		waitForPushDrain(ctx, server)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("waitForPushDrain returned before pushes drained")
	case <-time.After(50 * time.Millisecond):
	}

	server.MarkPushInFlightForTest(-1)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitForPushDrain did not finish after drain")
	}
}

func TestTriggerShutdownSignalsCompletionWithoutSignal(t *testing.T) {
	shutdownDone := make(chan struct{})
	var shutdownOnce sync.Once
	triggerShutdown := func(fn func()) {
		shutdownOnce.Do(func() {
			defer close(shutdownDone)
			fn()
		})
	}
	triggerShutdown(func() {})
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not signal completion")
	}
}
