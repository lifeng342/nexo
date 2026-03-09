package gateway

import (
	"context"
	"strconv"
	"time"

	"github.com/mbeoliero/kit/log"
	"github.com/mbeoliero/nexo/pkg/constant"
	"github.com/redis/go-redis/v9"
)

type InstanceState struct {
	InstanceID string
	Ready      bool
	Routeable  bool
	Draining   bool
}

type InstanceManager struct {
	rdb        *redis.Client
	instanceID string
	ttl        time.Duration
}

func (m *InstanceManager) SnapshotState(gate LifecycleGate) InstanceState {
	return InstanceState{InstanceID: m.instanceID, Ready: gate.IsReady(), Routeable: gate.IsRouteable(), Draining: gate.Snapshot().Draining}
}

func NewInstanceManager(rdb *redis.Client, instanceID string, ttl time.Duration) *InstanceManager {
	return &InstanceManager{rdb: rdb, instanceID: instanceID, ttl: ttl}
}

func instanceAliveKey(instanceID string) string {
	return constant.GetRedisKeyPrefix() + "instance:alive:" + instanceID
}

func boolToRedis(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func redisToBool(v string) bool {
	return v == "1"
}

func writeInstanceAlive(ctx context.Context, rdb *redis.Client, instanceID string, ready, routeable, draining bool) error {
	key := instanceAliveKey(instanceID)
	fields := map[string]any{
		"instance_id": instanceID,
		"ready":       boolToRedis(ready),
		"routeable":   boolToRedis(routeable),
		"draining":    boolToRedis(draining),
	}
	if err := rdb.HSet(ctx, key, fields).Err(); err != nil {
		return err
	}
	return rdb.Expire(ctx, key, time.Minute).Err()
}

func (m *InstanceManager) UpdateState(ctx context.Context, state InstanceState) error {
	if state.InstanceID == "" {
		state.InstanceID = m.instanceID
	}
	key := instanceAliveKey(state.InstanceID)
	fields := map[string]any{
		"instance_id": state.InstanceID,
		"ready":       boolToRedis(state.Ready),
		"routeable":   boolToRedis(state.Routeable),
		"draining":    boolToRedis(state.Draining),
	}
	if err := m.rdb.HSet(ctx, key, fields).Err(); err != nil {
		return err
	}
	return m.rdb.Expire(ctx, key, m.ttl).Err()
}

func (m *InstanceManager) Start(ctx context.Context, gate LifecycleGate, heartbeat time.Duration) {
	if heartbeat <= 0 {
		heartbeat = time.Second
	}
	go func() {
		ticker := time.NewTicker(heartbeat)
		defer ticker.Stop()
		if err := m.UpdateState(ctx, m.SnapshotState(gate)); err != nil {
			log.CtxWarn(ctx, "instance manager initial heartbeat failed: instance_id=%s, error=%v", m.instanceID, err)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := m.UpdateState(ctx, m.SnapshotState(gate)); err != nil {
					log.CtxWarn(ctx, "instance manager heartbeat failed: instance_id=%s, error=%v", m.instanceID, err)
				}
			}
		}
	}()
}

func (m *InstanceManager) GetState(ctx context.Context, instanceID string) (*InstanceState, error) {
	values, err := m.rdb.HGetAll(ctx, instanceAliveKey(instanceID)).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, redis.Nil
	}
	return &InstanceState{
		InstanceID: values["instance_id"],
		Ready:      redisToBool(values["ready"]),
		Routeable:  redisToBool(values["routeable"]),
		Draining:   redisToBool(values["draining"]),
	}, nil
}

func parseRedisInt(v string) int {
	i, _ := strconv.Atoi(v)
	return i
}
