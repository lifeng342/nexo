package gateway

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/pkg/constant"
)

// InstanceManager mirrors local lifecycle state to the instance_alive key.
type InstanceManager struct {
	rdb            *redis.Client
	cfg            config.CrossInstanceConfig
	instanceId     string
	gate           LifecycleGate
	connCountFn    func() int64
	startedAt      int64
	broadcastReady atomic.Bool
	syncHook       func(context.Context, instanceStatePayload) error
}

type instanceStatePayload struct {
	Ready          bool
	Routeable      bool
	BroadcastReady bool
	Draining       bool
	ConnCount      int64
}

// NewInstanceManager creates an instance manager.
func NewInstanceManager(rdb *redis.Client, cfg config.CrossInstanceConfig, instanceId string, gate LifecycleGate, connCountFn func() int64) *InstanceManager {
	manager := &InstanceManager{
		rdb:         rdb,
		cfg:         cfg,
		instanceId:  instanceId,
		gate:        gate,
		connCountFn: connCountFn,
		startedAt:   time.Now().UnixMilli(),
	}
	// Do not advertise broadcast capability until the first broadcast
	// subscription is actually established. Otherwise peers may choose
	// broadcast for an instance that is not consuming the shared channel yet.
	manager.broadcastReady.Store(false)
	return manager
}

// Run starts the heartbeat loop.
func (m *InstanceManager) Run(ctx context.Context) {
	if m == nil || m.rdb == nil {
		return
	}

	ticker := time.NewTicker(m.cfg.HeartbeatInterval())
	defer ticker.Stop()

	_ = m.Sync(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.Sync(ctx)
		}
	}
}

// Sync writes the current instance state immediately.
func (m *InstanceManager) Sync(ctx context.Context) error {
	if m == nil || m.rdb == nil {
		if m != nil && m.syncHook != nil {
			return m.syncHook(ctx, m.currentPayload())
		}
		return nil
	}

	return m.syncPayload(ctx, m.currentPayload())
}

// SyncSnapshot writes a projected lifecycle state without requiring the gate
// itself to switch first.
func (m *InstanceManager) SyncSnapshot(ctx context.Context, snap LifecycleSnapshot) error {
	if m == nil || m.rdb == nil {
		if m != nil && m.syncHook != nil {
			return m.syncHook(ctx, m.payloadForSnapshot(snap))
		}
		return nil
	}

	return m.syncPayload(ctx, m.payloadForSnapshot(snap))
}

func (m *InstanceManager) BroadcastReady() bool {
	if m == nil {
		return false
	}
	return m.broadcastReady.Load()
}

func (m *InstanceManager) currentPayload() instanceStatePayload {
	if m == nil {
		return instanceStatePayload{}
	}
	return m.payloadForSnapshot(m.gate.Snapshot())
}

func (m *InstanceManager) payloadForSnapshot(snap LifecycleSnapshot) instanceStatePayload {
	if m == nil {
		return instanceStatePayload{
			Ready:     snap.Ready,
			Routeable: snap.Routeable,
			Draining:  snap.Draining,
		}
	}
	var connCount int64
	if m.connCountFn != nil {
		connCount = m.connCountFn()
	}
	return instanceStatePayload{
		Ready:          snap.Ready,
		Routeable:      snap.Routeable,
		BroadcastReady: m.broadcastReady.Load(),
		Draining:       snap.Draining,
		ConnCount:      connCount,
	}
}

func (m *InstanceManager) syncPayload(ctx context.Context, payload instanceStatePayload) error {
	if m == nil {
		return nil
	}
	if m.syncHook != nil {
		return m.syncHook(ctx, payload)
	}

	key := fmt.Sprintf(constant.RedisKeyInstanceAlive(), m.instanceId)
	if err := m.rdb.HSet(ctx, key, map[string]any{
		"instance_id":     m.instanceId,
		"ready":           boolToRedis(payload.Ready),
		"routeable":       boolToRedis(payload.Routeable),
		"broadcast_ready": boolToRedis(payload.BroadcastReady),
		"draining":        boolToRedis(payload.Draining),
		"started_at":      m.startedAt,
		"last_heartbeat":  time.Now().UnixMilli(),
		"conn_count":      payload.ConnCount,
	}).Err(); err != nil {
		return err
	}
	return m.rdb.Expire(ctx, key, m.cfg.InstanceAliveTTL()).Err()
}

// Remove removes the instance_alive key.
func (m *InstanceManager) Remove(ctx context.Context) error {
	if m == nil || m.rdb == nil {
		return nil
	}
	return m.rdb.Del(ctx, fmt.Sprintf(constant.RedisKeyInstanceAlive(), m.instanceId)).Err()
}

// SetBroadcastReady updates the broadcast-ready mirror bit.
func (m *InstanceManager) SetBroadcastReady(ready bool) {
	if m == nil {
		return
	}
	m.broadcastReady.Store(ready)
}

// AllBroadcastReady returns whether every target instance can still receive broadcast.
func (m *InstanceManager) AllBroadcastReady(ctx context.Context, instanceIds []string) bool {
	states, err := loadInstanceStates(ctx, m.rdb, instanceIds)
	if err != nil {
		return false
	}
	for _, instanceId := range instanceIds {
		state := states[instanceId]
		if !state.Alive || !state.BroadcastReady {
			return false
		}
	}
	return true
}
