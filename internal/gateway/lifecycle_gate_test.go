package gateway

import (
	"testing"

	"github.com/mbeoliero/nexo/pkg/errcode"
)

func TestLifecycleGateAcquireSendLease(t *testing.T) {
	gate := NewLifecycleGate()

	release, err := gate.AcquireSendLease()
	if err != nil {
		t.Fatalf("AcquireSendLease() error = %v", err)
	}
	if got := gate.Snapshot().InflightSend; got != 1 {
		t.Fatalf("Snapshot().InflightSend = %d, want 1", got)
	}

	gate.BeginSendDrain()

	if _, err := gate.AcquireSendLease(); err != errcode.ErrServerShuttingDown {
		t.Fatalf("AcquireSendLease() after BeginSendDrain error = %v, want %v", err, errcode.ErrServerShuttingDown)
	}

	release()
	if got := gate.Snapshot().InflightSend; got != 0 {
		t.Fatalf("Snapshot().InflightSend = %d, want 0", got)
	}
}

func TestLifecycleGateReadiness(t *testing.T) {
	gate := NewLifecycleGate()

	if !gate.CanAcceptIngress() {
		t.Fatal("CanAcceptIngress() = false, want true")
	}
	if !gate.IsReady() {
		t.Fatal("IsReady() = false, want true")
	}
	if !gate.IsRouteable() {
		t.Fatal("IsRouteable() = false, want true")
	}

	gate.MarkUnready()
	gate.CloseIngress()
	gate.EnterDraining()
	gate.MarkUnrouteable()
	gate.CloseSendPath()

	snap := gate.Snapshot()
	if snap.Ready {
		t.Fatal("Snapshot().Ready = true, want false")
	}
	if snap.Routeable {
		t.Fatal("Snapshot().Routeable = true, want false")
	}
	if !snap.IngressClosed {
		t.Fatal("Snapshot().IngressClosed = false, want true")
	}
	if !snap.SendClosed {
		t.Fatal("Snapshot().SendClosed = false, want true")
	}
	if !snap.Draining {
		t.Fatal("Snapshot().Draining = false, want true")
	}
}
