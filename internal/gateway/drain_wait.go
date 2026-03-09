package gateway

import (
	"context"
	"time"
)

func waitForSendDrainForTest(ctx context.Context, gate LifecycleGate) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if gate.Snapshot().InflightSend == 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
