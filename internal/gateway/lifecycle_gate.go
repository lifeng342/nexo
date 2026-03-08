package gateway

import (
	"sync"
	"sync/atomic"

	"github.com/mbeoliero/nexo/pkg/errcode"
)

// LifecycleSnapshot is the current lifecycle gate view.
type LifecycleSnapshot struct {
	Ready         bool
	Routeable     bool
	IngressClosed bool
	SendDraining  bool
	SendClosed    bool
	Draining      bool
	InflightSend  int64
}

// DrainReason describes why the current instance enters drain mode.
type DrainReason string

const (
	DrainReasonPlannedUpgrade DrainReason = "planned_upgrade"
	DrainReasonSubscribeFault DrainReason = "subscribe_fault"
	DrainReasonManual         DrainReason = "manual"
)

// LifecycleGate controls ingress, routeability and send path state.
type LifecycleGate interface {
	CanAcceptIngress() bool
	CanStartSend() bool
	AcquireSendLease() (release func(), err error)
	IsReady() bool
	IsRouteable() bool
	Snapshot() LifecycleSnapshot
	MarkReady()
	MarkRouteable()
	MarkUnready()
	MarkUnrouteable()
	CloseIngress()
	BeginSendDrain()
	CloseSendPath()
	EnterDraining()
}

// LocalLifecycleGate is the in-process lifecycle source of truth.
type LocalLifecycleGate struct {
	ready         atomic.Bool
	routeable     atomic.Bool
	ingressClosed atomic.Bool
	sendDraining  atomic.Bool
	sendClosed    atomic.Bool
	draining      atomic.Bool
	inflightSend  atomic.Int64
}

// NewLifecycleGate creates a gate in ready/routeable state.
func NewLifecycleGate() *LocalLifecycleGate {
	return NewLifecycleGateWithState(true, true)
}

// NewLifecycleGateWithState creates a gate with explicit ready/routeable state.
func NewLifecycleGateWithState(ready bool, routeable bool) *LocalLifecycleGate {
	gate := &LocalLifecycleGate{}
	gate.ready.Store(ready)
	gate.routeable.Store(routeable)
	return gate
}

// CanAcceptIngress returns whether new HTTP/WS ingress is still allowed.
func (g *LocalLifecycleGate) CanAcceptIngress() bool {
	return g.ready.Load() && !g.ingressClosed.Load()
}

// CanStartSend returns whether new send requests may still start.
func (g *LocalLifecycleGate) CanStartSend() bool {
	return !g.sendDraining.Load() && !g.sendClosed.Load()
}

// AcquireSendLease reserves one in-flight send slot.
func (g *LocalLifecycleGate) AcquireSendLease() (func(), error) {
	if !g.CanStartSend() {
		return nil, errcode.ErrServerShuttingDown
	}

	g.inflightSend.Add(1)
	if !g.CanStartSend() {
		g.inflightSend.Add(-1)
		return nil, errcode.ErrServerShuttingDown
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			g.inflightSend.Add(-1)
		})
	}, nil
}

// IsReady returns whether the instance is still ready for new traffic.
func (g *LocalLifecycleGate) IsReady() bool {
	return g.ready.Load()
}

// IsRouteable returns whether the instance may still be used as a route target.
func (g *LocalLifecycleGate) IsRouteable() bool {
	return g.routeable.Load()
}

// Snapshot returns the current state.
func (g *LocalLifecycleGate) Snapshot() LifecycleSnapshot {
	return LifecycleSnapshot{
		Ready:         g.ready.Load(),
		Routeable:     g.routeable.Load(),
		IngressClosed: g.ingressClosed.Load(),
		SendDraining:  g.sendDraining.Load(),
		SendClosed:    g.sendClosed.Load(),
		Draining:      g.draining.Load(),
		InflightSend:  g.inflightSend.Load(),
	}
}

// MarkReady allows the instance to receive new traffic again.
func (g *LocalLifecycleGate) MarkReady() {
	g.ready.Store(true)
}

// MarkUnready stops the instance from receiving new traffic.
func (g *LocalLifecycleGate) MarkUnready() {
	g.ready.Store(false)
}

// MarkRouteable restores the instance as a route target.
func (g *LocalLifecycleGate) MarkRouteable() {
	g.routeable.Store(true)
}

// MarkUnrouteable removes the instance from cross-instance route targets.
func (g *LocalLifecycleGate) MarkUnrouteable() {
	g.routeable.Store(false)
}

// CloseIngress blocks new ingress.
func (g *LocalLifecycleGate) CloseIngress() {
	g.ingressClosed.Store(true)
}

// BeginSendDrain rejects new send leases while letting existing sends finish.
func (g *LocalLifecycleGate) BeginSendDrain() {
	g.sendDraining.Store(true)
}

// CloseSendPath hard-closes the send path for new async push tasks.
func (g *LocalLifecycleGate) CloseSendPath() {
	g.sendClosed.Store(true)
}

// EnterDraining marks the instance as actively draining.
func (g *LocalLifecycleGate) EnterDraining() {
	g.draining.Store(true)
}
