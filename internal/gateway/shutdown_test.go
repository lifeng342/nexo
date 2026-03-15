package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/stretchr/testify/require"
)

func TestShutdownRejectsNewSendsButAllowsInflightLeaseToFinish(t *testing.T) {
	gate := NewLifecycleGate()
	release, err := gate.AcquireSendLease()
	require.NoError(t, err)
	gate.BeginSendDrain()
	_, err = gate.AcquireSendLease()
	require.ErrorIs(t, err, errcode.ErrServerShuttingDown)
	release()
	gate.CloseSendPath()
	require.True(t, gate.Snapshot().SendClosed)
}

func TestPlannedDrainKeepsRouteableTrueUntilConnectionsDrain(t *testing.T) {
	gate := NewLifecycleGate()
	gate.MarkUnready()
	gate.CloseIngress()
	gate.BeginSendDrain()
	gate.EnterDraining()
	require.False(t, gate.IsReady())
	require.True(t, gate.IsRouteable())
}

func TestSubscribeFaultMarksInstanceUnrouteableImmediately(t *testing.T) {
	gate := NewLifecycleGate()
	gate.MarkUnready()
	gate.MarkUnrouteable()
	gate.CloseIngress()
	gate.BeginSendDrain()
	gate.EnterDraining()
	require.False(t, gate.IsRouteable())
}

func TestUnregisterClientFallsBackWhenChannelFull(t *testing.T) {
	server := &WsServer{unregisterChan: make(chan *Client), userMap: NewUserMap(nil)}
	client := &Client{UserId: "u1", ConnId: "c1"}
	server.userMap.Register(context.Background(), client)
	server.UnregisterClient(client)
	_, ok := server.userMap.GetAll("u1")
	require.False(t, ok)
}

func TestWaitForSendDrainClosesAfterInflightCompletes(t *testing.T) {
	gate := NewLifecycleGate()
	release, err := gate.AcquireSendLease()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		waitForSendDrainForTest(ctx, gate)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitForSendDrain did not finish")
	}
}

func TestDrainLocalClientsKicksInBatchesAndWaitsForZero(t *testing.T) {
	server := newDrainTestServer(3)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result := server.DrainLocalClients(ctx, DrainLocalClientsOptions{BatchSize: 2, BatchInterval: time.Millisecond, SettleTimeout: 5 * time.Millisecond})

	require.Equal(t, int64(0), server.GetOnlineConnCount())
	require.Equal(t, 3, result.KickAttempts)
	require.Equal(t, 0, result.FallbackCloses)
	require.Equal(t, 0, result.Survivors)
}

func TestDrainLocalClientsForceClosesSurvivorsAfterTimeout(t *testing.T) {
	server := newDrainTestServer(2)
	server.SetDrainUnregisterOnKick(false)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := server.DrainLocalClients(ctx, DrainLocalClientsOptions{BatchSize: 1, BatchInterval: time.Millisecond, DrainTimeout: 5 * time.Millisecond, SettleTimeout: 5 * time.Millisecond})

	require.Equal(t, 2, result.KickAttempts)
	require.GreaterOrEqual(t, result.FallbackCloses, 2)
	require.Equal(t, int64(0), server.GetOnlineConnCount())
}

func TestDrainLocalClientsStillForceClosesOnContextDone(t *testing.T) {
	server := newDrainTestServer(2)
	server.SetDrainUnregisterOnKick(false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := server.DrainLocalClients(ctx, DrainLocalClientsOptions{BatchSize: 1, BatchInterval: time.Second, DrainTimeout: time.Second})

	require.GreaterOrEqual(t, result.FallbackCloses, 2)
	require.Equal(t, int64(0), server.GetOnlineConnCount())
}

func TestDrainLocalClientsCatchesLateRegisteredClient(t *testing.T) {
	server := newDrainTestServer(1)
	lateClient := NewClient(&drainTestConn{}, "u2", 2, "", "", "late", server)
	server.registerChan = make(chan *Client, 1)
	server.registerChan <- lateClient

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := server.DrainLocalClients(ctx, DrainLocalClientsOptions{BatchSize: 1, BatchInterval: time.Millisecond, DrainTimeout: 5 * time.Millisecond, SettleTimeout: 5 * time.Millisecond})

	require.GreaterOrEqual(t, result.KickAttempts, 2)
	require.Equal(t, int64(0), server.GetOnlineConnCount())
}
