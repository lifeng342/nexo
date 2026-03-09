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
