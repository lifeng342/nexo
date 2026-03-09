package gateway

import (
	"testing"

	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/stretchr/testify/require"
)

func TestLifecycleGateAcquireSendLease(t *testing.T) {
	gate := NewLifecycleGate()
	release, err := gate.AcquireSendLease()
	require.NoError(t, err)
	require.NotNil(t, release)
	require.Equal(t, int64(1), gate.Snapshot().InflightSend)
	release()
	require.Equal(t, int64(0), gate.Snapshot().InflightSend)
}

func TestLifecycleGateBeginSendDrainRejectsNewLease(t *testing.T) {
	gate := NewLifecycleGate()
	gate.BeginSendDrain()
	_, err := gate.AcquireSendLease()
	require.ErrorIs(t, err, errcode.ErrServerShuttingDown)
}

func TestLifecycleGateCloseSendPathAfterInflightDrains(t *testing.T) {
	gate := NewLifecycleGate()
	release, err := gate.AcquireSendLease()
	require.NoError(t, err)
	gate.BeginSendDrain()
	require.False(t, gate.Snapshot().SendClosed)
	release()
	gate.CloseSendPath()
	require.True(t, gate.Snapshot().SendClosed)
}
