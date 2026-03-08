package gateway

import (
	"testing"

	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/pkg/constant"
)

func TestHybridSelectorSelect(t *testing.T) {
	selector := NewHybridSelector(config.HybridConfig{
		Enabled:                true,
		GroupRouteMaxTargets:   100,
		GroupRouteMaxInstances: 4,
	})

	tests := []struct {
		name          string
		sessionType   int32
		targetUsers   int
		instanceCount int
		want          PushMode
	}{
		{
			name:          "single chat always route",
			sessionType:   constant.SessionTypeSingle,
			targetUsers:   1,
			instanceCount: 1,
			want:          PushModeRoute,
		},
		{
			name:          "small group prefers route",
			sessionType:   constant.SessionTypeGroup,
			targetUsers:   20,
			instanceCount: 2,
			want:          PushModeRoute,
		},
		{
			name:          "large group falls back to broadcast",
			sessionType:   constant.SessionTypeGroup,
			targetUsers:   120,
			instanceCount: 5,
			want:          PushModeBroadcast,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selector.Select(tt.sessionType, tt.targetUsers, tt.instanceCount); got != tt.want {
				t.Fatalf("Select() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHybridSelectorForceBroadcast(t *testing.T) {
	selector := NewHybridSelector(config.HybridConfig{
		Enabled:        true,
		ForceBroadcast: true,
	})

	if got := selector.Select(constant.SessionTypeGroup, 2, 1); got != PushModeBroadcast {
		t.Fatalf("Select() = %q, want %q", got, PushModeBroadcast)
	}
	if got := selector.Select(constant.SessionTypeSingle, 1, 1); got != PushModeRoute {
		t.Fatalf("Select() single = %q, want %q", got, PushModeRoute)
	}
}
