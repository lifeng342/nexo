package gateway

import (
	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/pkg/constant"
)

// HybridSelector chooses between route and broadcast based on route results.
type HybridSelector struct {
	cfg config.HybridConfig
}

// NewHybridSelector creates a route/broadcast selector.
func NewHybridSelector(cfg config.HybridConfig) *HybridSelector {
	return &HybridSelector{cfg: cfg}
}

// Select chooses the push mode for the current message.
func (s *HybridSelector) Select(sessionType int32, targetUsers int, instanceCount int) PushMode {
	if sessionType == constant.SessionTypeSingle {
		return PushModeRoute
	}
	if s.cfg.ForceBroadcast {
		return PushModeBroadcast
	}
	if !s.cfg.Enabled {
		return PushModeRoute
	}
	if targetUsers <= s.cfg.GroupRouteMaxTargets && instanceCount <= s.cfg.GroupRouteMaxInstances {
		return PushModeRoute
	}
	return PushModeBroadcast
}
