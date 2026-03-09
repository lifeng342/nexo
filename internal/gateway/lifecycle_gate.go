package gateway

import (
	"sync"
	"sync/atomic"

	"github.com/mbeoliero/nexo/pkg/errcode"
)

type LifecycleSnapshot struct {
	Ready         bool
	Routeable     bool
	IngressClosed bool
	SendDraining  bool
	SendClosed    bool
	Draining      bool
	InflightSend  int64
}

type DrainReason string

const (
	DrainReasonPlannedUpgrade DrainReason = "planned_upgrade"
	DrainReasonSubscribeFault DrainReason = "subscribe_fault"
	DrainReasonManual         DrainReason = "manual"
)

type LifecycleGate interface {
	CanAcceptIngress() bool
	CanStartSend() bool
	AcquireSendLease() (release func(), err error)
	IsReady() bool
	IsRouteable() bool
	Snapshot() LifecycleSnapshot
	MarkUnready()
	MarkUnrouteable()
	CloseIngress()
	BeginSendDrain()
	CloseSendPath()
	EnterDraining()
	ForceCloseSendPath()
}

type lifecycleGate struct {
	ready         atomic.Bool
	routeable     atomic.Bool
	ingressClosed atomic.Bool
	sendDraining  atomic.Bool
	sendClosed    atomic.Bool
	draining      atomic.Bool
	inflightSend  atomic.Int64
	releaseMu     sync.Mutex
}

func NewLifecycleGate() LifecycleGate {
	g := &lifecycleGate{}
	g.ready.Store(true)
	g.routeable.Store(true)
	return g
}

func (g *lifecycleGate) CanAcceptIngress() bool {
	return g.ready.Load() && !g.ingressClosed.Load()
}

func (g *lifecycleGate) CanStartSend() bool {
	return !g.sendDraining.Load() && !g.sendClosed.Load()
}

func (g *lifecycleGate) AcquireSendLease() (func(), error) {
	g.releaseMu.Lock()
	defer g.releaseMu.Unlock()
	if g.sendDraining.Load() || g.sendClosed.Load() {
		return nil, errcode.ErrServerShuttingDown
	}
	g.inflightSend.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			g.inflightSend.Add(-1)
		})
	}, nil
}

func (g *lifecycleGate) IsReady() bool {
	return g.ready.Load()
}

func (g *lifecycleGate) IsRouteable() bool {
	return g.routeable.Load()
}

func (g *lifecycleGate) Snapshot() LifecycleSnapshot {
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

func (g *lifecycleGate) MarkUnready() {
	g.ready.Store(false)
}

func (g *lifecycleGate) MarkUnrouteable() {
	g.routeable.Store(false)
}

func (g *lifecycleGate) CloseIngress() {
	g.ingressClosed.Store(true)
}

func (g *lifecycleGate) BeginSendDrain() {
	g.sendDraining.Store(true)
}

func (g *lifecycleGate) CloseSendPath() {
	if g.inflightSend.Load() > 0 {
		return
	}
	g.sendClosed.Store(true)
}

func (g *lifecycleGate) ForceCloseSendPath() {
	g.sendClosed.Store(true)
}

func (g *lifecycleGate) EnterDraining() {
	g.draining.Store(true)
}
