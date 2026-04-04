package cluster

import (
	"context"
	"sync"
	"time"

	"github.com/mbeoliero/nexo/pkg/errcode"
)

type LifecyclePhase string

const (
	LifecyclePhaseServing    LifecyclePhase = "serving"
	LifecyclePhaseDraining   LifecyclePhase = "draining"
	LifecyclePhaseSendClosed LifecyclePhase = "send_closed"
	LifecyclePhaseStopped    LifecyclePhase = "stopped"
)

type DrainReason string

const (
	DrainReasonPlanned             DrainReason = "planned"
	DrainReasonSubscribeFault      DrainReason = "subscribe_fault"
	DrainReasonStatePublishFault   DrainReason = "state_publish_fault"
	DrainReasonReconcileFault      DrainReason = "reconcile_fault"
	DrainReasonRouteReadFault      DrainReason = "route_read_fault"
	DrainReasonRouteMirrorOverload DrainReason = "route_mirror_overload"
)

type LifecycleSnapshot struct {
	Phase         LifecyclePhase `json:"phase"`
	Ready         bool           `json:"ready"`
	Routeable     bool           `json:"routeable"`
	IngressClosed bool           `json:"ingress_closed"`
	InflightSend  int64          `json:"inflight_send"`
	DrainReason   DrainReason    `json:"drain_reason"`
}

type LifecycleGate interface {
	Snapshot() LifecycleSnapshot
	CanAcceptIngress() bool
	AcquireSendLease() (func(), error)
	BeginDrain(reason DrainReason) error
	MarkDegraded(reason DrainReason) error
	MarkHealthy() error
	MarkRouteableOff() error
	WaitInflightZero(ctx context.Context) error
	CloseSendPath() error
	ForceCloseSendPath() error
	MarkStopped() error
}

// BasicLifecycleGate implements the phase-1 lifecycle state machine.
type BasicLifecycleGate struct {
	mu       sync.Mutex
	cond     *sync.Cond
	snapshot LifecycleSnapshot
}

func NewLifecycleGate() *BasicLifecycleGate {
	gate := &BasicLifecycleGate{
		snapshot: LifecycleSnapshot{
			Phase:     LifecyclePhaseServing,
			Ready:     true,
			Routeable: true,
		},
	}
	gate.cond = sync.NewCond(&gate.mu)
	return gate
}

func (g *BasicLifecycleGate) Snapshot() LifecycleSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snapshot
}

func (g *BasicLifecycleGate) CanAcceptIngress() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.snapshot.IngressClosed
}

func (g *BasicLifecycleGate) AcquireSendLease() (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.snapshot.Phase != LifecyclePhaseServing {
		return nil, errcode.ErrServerShuttingDown
	}

	g.snapshot.InflightSend++
	released := false
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if released {
			return
		}
		released = true
		if g.snapshot.InflightSend > 0 {
			g.snapshot.InflightSend--
		}
		g.cond.Broadcast()
	}, nil
}

func (g *BasicLifecycleGate) BeginDrain(reason DrainReason) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.snapshot.Phase == LifecyclePhaseStopped || g.snapshot.Phase == LifecyclePhaseSendClosed {
		return nil
	}

	g.snapshot.Phase = LifecyclePhaseDraining
	g.snapshot.Ready = false
	g.snapshot.IngressClosed = true
	g.snapshot.DrainReason = reason
	g.snapshot.Routeable = drainReasonKeepsRouteable(reason)
	g.cond.Broadcast()
	return nil
}

func (g *BasicLifecycleGate) MarkDegraded(reason DrainReason) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.snapshot.Phase == LifecyclePhaseStopped || g.snapshot.Phase == LifecyclePhaseSendClosed {
		return nil
	}

	g.snapshot.Ready = false
	g.snapshot.Routeable = false
	g.snapshot.DrainReason = reason
	g.cond.Broadcast()
	return nil
}

func drainReasonKeepsRouteable(reason DrainReason) bool {
	switch reason {
	case DrainReasonSubscribeFault, DrainReasonStatePublishFault, DrainReasonReconcileFault, DrainReasonRouteReadFault, DrainReasonRouteMirrorOverload:
		return false
	default:
		return true
	}
}

func (g *BasicLifecycleGate) MarkHealthy() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.snapshot.Phase != LifecyclePhaseServing {
		return nil
	}

	g.snapshot.Ready = true
	g.snapshot.Routeable = true
	g.snapshot.IngressClosed = false
	g.snapshot.DrainReason = ""
	g.cond.Broadcast()
	return nil
}

func (g *BasicLifecycleGate) MarkRouteableOff() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.snapshot.Routeable = false
	g.cond.Broadcast()
	return nil
}

func (g *BasicLifecycleGate) WaitInflightZero(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		g.mu.Lock()
		inflight := g.snapshot.InflightSend
		g.mu.Unlock()
		if inflight == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (g *BasicLifecycleGate) CloseSendPath() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	for g.snapshot.InflightSend > 0 {
		g.cond.Wait()
	}

	if g.snapshot.Phase == LifecyclePhaseStopped {
		return nil
	}

	g.snapshot.Phase = LifecyclePhaseSendClosed
	g.snapshot.Ready = false
	g.snapshot.Routeable = false
	g.snapshot.IngressClosed = true
	g.cond.Broadcast()
	return nil
}

func (g *BasicLifecycleGate) ForceCloseSendPath() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.snapshot.Phase == LifecyclePhaseStopped {
		return nil
	}

	g.snapshot.Phase = LifecyclePhaseSendClosed
	g.snapshot.Ready = false
	g.snapshot.Routeable = false
	g.snapshot.IngressClosed = true
	g.cond.Broadcast()
	return nil
}

func (g *BasicLifecycleGate) MarkStopped() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.snapshot.Phase = LifecyclePhaseStopped
	g.snapshot.Ready = false
	g.snapshot.Routeable = false
	g.snapshot.IngressClosed = true
	g.cond.Broadcast()
	return nil
}
