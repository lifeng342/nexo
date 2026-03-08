package gateway

import "sync/atomic"

// CapabilityState describes the health of one cross-instance capability.
type CapabilityState int32

const (
	CapabilityDisabled CapabilityState = iota
	CapabilityReady
	CapabilityDegraded
)

// CrossInstanceCapabilities holds the fine-grained cross-instance switches.
type CrossInstanceCapabilities struct {
	Publish            atomic.Int32
	RouteSubscribe     atomic.Int32
	BroadcastSubscribe atomic.Int32
	PushRouteRead      atomic.Int32
	PresenceRead       atomic.Int32
}

// NewCrossInstanceCapabilities creates a capability set.
func NewCrossInstanceCapabilities(enabled bool) *CrossInstanceCapabilities {
	caps := &CrossInstanceCapabilities{}
	state := CapabilityDisabled
	if enabled {
		state = CapabilityReady
	}
	caps.Publish.Store(int32(state))
	caps.RouteSubscribe.Store(int32(state))
	caps.BroadcastSubscribe.Store(int32(state))
	caps.PushRouteRead.Store(int32(state))
	caps.PresenceRead.Store(int32(state))
	return caps
}

// CanPublishOutbound returns whether outbound remote publish is enabled.
func (c *CrossInstanceCapabilities) CanPublishOutbound() bool {
	return c.Publish.Load() == int32(CapabilityReady)
}

// CanReceiveRouteEnvelope returns whether the instance subscription is healthy.
func (c *CrossInstanceCapabilities) CanReceiveRouteEnvelope() bool {
	return c.RouteSubscribe.Load() == int32(CapabilityReady)
}

// CanReceiveBroadcastEnvelope returns whether the broadcast subscription is healthy.
func (c *CrossInstanceCapabilities) CanReceiveBroadcastEnvelope() bool {
	return c.BroadcastSubscribe.Load() == int32(CapabilityReady)
}

// CanReadPushRoute returns whether push route reads are healthy.
func (c *CrossInstanceCapabilities) CanReadPushRoute() bool {
	return c.PushRouteRead.Load() == int32(CapabilityReady)
}

// CanReadPresence returns whether presence reads are healthy.
func (c *CrossInstanceCapabilities) CanReadPresence() bool {
	return c.PresenceRead.Load() == int32(CapabilityReady)
}
