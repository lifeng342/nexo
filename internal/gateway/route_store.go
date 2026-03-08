package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mbeoliero/nexo/internal/config"
	"github.com/mbeoliero/nexo/pkg/constant"
)

// RouteStore keeps distributed route and presence state in Redis.
type RouteStore struct {
	rdb               *redis.Client
	routeTTL          time.Duration
	staleCleanupLimit int
}

// InstanceState is the route filter state loaded from Redis.
type InstanceState struct {
	InstanceId     string
	Alive          bool
	Ready          bool
	Routeable      bool
	BroadcastReady bool
	Draining       bool
}

// NewRouteStore creates a new Redis-backed route store.
func NewRouteStore(rdb *redis.Client, cfg *config.Config) *RouteStore {
	return &RouteStore{
		rdb:               rdb,
		routeTTL:          cfg.WebSocket.CrossInstance.RouteTTL(),
		staleCleanupLimit: cfg.WebSocket.RouteStaleCleanupLimit,
	}
}

// RegisterConn registers one connection in the user route hash and instance index.
func (s *RouteStore) RegisterConn(ctx context.Context, conn RouteConn) error {
	if s == nil || s.rdb == nil {
		return nil
	}

	userKey := fmt.Sprintf(constant.RedisKeyRouteUser(), conn.UserId)
	instKey := fmt.Sprintf(constant.RedisKeyRouteInstance(), conn.InstanceId)
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, userKey, conn.ConnId, formatRouteValue(conn))
	pipe.Expire(ctx, userKey, s.routeTTL)
	pipe.SAdd(ctx, instKey, formatInstanceRouteMember(conn))
	pipe.Expire(ctx, instKey, s.routeTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// UnregisterConn removes one connection from the user route hash and instance index.
func (s *RouteStore) UnregisterConn(ctx context.Context, conn RouteConn) error {
	if s == nil || s.rdb == nil {
		return nil
	}

	userKey := fmt.Sprintf(constant.RedisKeyRouteUser(), conn.UserId)
	instKey := fmt.Sprintf(constant.RedisKeyRouteInstance(), conn.InstanceId)
	pipe := s.rdb.TxPipeline()
	pipe.HDel(ctx, userKey, conn.ConnId)
	pipe.SRem(ctx, instKey, formatInstanceRouteMember(conn))
	_, err := pipe.Exec(ctx)
	return err
}

// GetUsersConnRefs loads routeable remote connections for push delivery.
func (s *RouteStore) GetUsersConnRefs(ctx context.Context, userIds []string) (map[string][]RouteConn, error) {
	routes, err := s.getUsersConnRefs(ctx, userIds)
	if err != nil {
		return nil, err
	}

	states, err := loadInstanceStates(ctx, s.rdb, collectInstanceIDs(routes))
	if err != nil {
		return nil, err
	}
	indexExists, err := loadInstanceRouteIndexExists(ctx, s.rdb, collectInstanceIDs(routes))
	if err != nil {
		return nil, err
	}

	result := make(map[string][]RouteConn, len(routes))
	var cleanup []RouteConn
	for userId, refs := range routes {
		for _, ref := range refs {
			state := states[ref.InstanceId]
			if state.Alive && state.Routeable {
				result[userId] = append(result[userId], ref)
				continue
			}
			// Missing alive state can be a transient heartbeat/read issue. Only clean
			// the route once both the instance heartbeat and its ownership index are gone.
			if !state.Alive && !indexExists[ref.InstanceId] && len(cleanup) < s.staleCleanupLimit {
				cleanup = append(cleanup, ref)
			}
		}
	}
	if len(cleanup) > 0 {
		_ = s.cleanupRoutes(ctx, cleanup)
	}
	return result, nil
}

// GetUsersPresenceConnRefs loads alive remote connections for online status aggregation.
func (s *RouteStore) GetUsersPresenceConnRefs(ctx context.Context, userIds []string) (map[string][]RouteConn, error) {
	routes, err := s.getUsersConnRefs(ctx, userIds)
	if err != nil {
		return nil, err
	}

	states, err := loadInstanceStates(ctx, s.rdb, collectInstanceIDs(routes))
	if err != nil {
		return nil, err
	}

	result := make(map[string][]RouteConn, len(routes))
	for userId, refs := range routes {
		for _, ref := range refs {
			if states[ref.InstanceId].Alive {
				result[userId] = append(result[userId], ref)
			}
		}
	}
	return result, nil
}

// RefreshInstanceRoutesTTL refreshes TTL for the current instance route keys.
func (s *RouteStore) RefreshInstanceRoutesTTL(ctx context.Context, instanceId string, conns []RouteConn) error {
	if s == nil || s.rdb == nil {
		return nil
	}

	pipe := s.rdb.TxPipeline()
	instKey := fmt.Sprintf(constant.RedisKeyRouteInstance(), instanceId)
	pipe.Expire(ctx, instKey, s.routeTTL)
	for userId := range uniqueUserIDs(conns) {
		userKey := fmt.Sprintf(constant.RedisKeyRouteUser(), userId)
		pipe.Expire(ctx, userKey, s.routeTTL)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// ReconcileInstanceRoutes repairs missing routes and removes stale instance-owned routes.
func (s *RouteStore) ReconcileInstanceRoutes(ctx context.Context, instanceId string, conns []RouteConn) error {
	if s == nil || s.rdb == nil {
		return nil
	}

	instKey := fmt.Sprintf(constant.RedisKeyRouteInstance(), instanceId)
	existingMembers, err := s.rdb.SMembers(ctx, instKey).Result()
	if err != nil && err != redis.Nil {
		return err
	}

	desired := make(map[string]RouteConn, len(conns))
	for _, conn := range conns {
		desired[formatInstanceRouteMember(conn)] = conn
	}
	existing := make(map[string]struct{}, len(existingMembers))
	for _, member := range existingMembers {
		existing[member] = struct{}{}
	}

	pipe := s.rdb.TxPipeline()
	pipe.Expire(ctx, instKey, s.routeTTL)

	for member, conn := range desired {
		userKey := fmt.Sprintf(constant.RedisKeyRouteUser(), conn.UserId)
		pipe.HSet(ctx, userKey, conn.ConnId, formatRouteValue(conn))
		pipe.Expire(ctx, userKey, s.routeTTL)
		if _, ok := existing[member]; !ok {
			pipe.SAdd(ctx, instKey, member)
		}
	}

	for member := range existing {
		if _, ok := desired[member]; ok {
			continue
		}
		userId, connId, ok := parseInstanceRouteMember(member)
		if !ok {
			pipe.SRem(ctx, instKey, member)
			continue
		}
		userKey := fmt.Sprintf(constant.RedisKeyRouteUser(), userId)
		pipe.HDel(ctx, userKey, connId)
		pipe.SRem(ctx, instKey, member)
	}

	_, err = pipe.Exec(ctx)
	return err
}

func (s *RouteStore) getUsersConnRefs(ctx context.Context, userIds []string) (map[string][]RouteConn, error) {
	if s == nil || s.rdb == nil || len(userIds) == 0 {
		return map[string][]RouteConn{}, nil
	}

	cmds := make(map[string]*redis.MapStringStringCmd, len(userIds))
	pipe := s.rdb.Pipeline()
	for _, userId := range userIds {
		key := fmt.Sprintf(constant.RedisKeyRouteUser(), userId)
		cmds[userId] = pipe.HGetAll(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	result := make(map[string][]RouteConn, len(userIds))
	for userId, cmd := range cmds {
		fields, err := cmd.Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		for connId, raw := range fields {
			ref, ok := parseRouteValue(userId, connId, raw)
			if !ok {
				continue
			}
			result[userId] = append(result[userId], ref)
		}
	}
	return result, nil
}

func (s *RouteStore) cleanupRoutes(ctx context.Context, conns []RouteConn) error {
	if len(conns) == 0 {
		return nil
	}

	pipe := s.rdb.Pipeline()
	for _, conn := range conns {
		userKey := fmt.Sprintf(constant.RedisKeyRouteUser(), conn.UserId)
		instKey := fmt.Sprintf(constant.RedisKeyRouteInstance(), conn.InstanceId)
		pipe.HDel(ctx, userKey, conn.ConnId)
		pipe.SRem(ctx, instKey, formatInstanceRouteMember(conn))
	}
	_, err := pipe.Exec(ctx)
	return err
}

func loadInstanceStates(ctx context.Context, rdb *redis.Client, instanceIds []string) (map[string]InstanceState, error) {
	result := make(map[string]InstanceState, len(instanceIds))
	if rdb == nil || len(instanceIds) == 0 {
		return result, nil
	}

	cmds := make(map[string]*redis.MapStringStringCmd, len(instanceIds))
	pipe := rdb.Pipeline()
	for _, instanceId := range instanceIds {
		key := fmt.Sprintf(constant.RedisKeyInstanceAlive(), instanceId)
		cmds[instanceId] = pipe.HGetAll(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	for instanceId, cmd := range cmds {
		values, err := cmd.Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		if len(values) == 0 {
			result[instanceId] = InstanceState{InstanceId: instanceId}
			continue
		}
		result[instanceId] = InstanceState{
			InstanceId:     instanceId,
			Alive:          true,
			Ready:          redisBool(values["ready"]),
			Routeable:      redisBool(values["routeable"]),
			BroadcastReady: redisBool(values["broadcast_ready"]),
			Draining:       redisBool(values["draining"]),
		}
	}
	return result, nil
}

func loadInstanceRouteIndexExists(ctx context.Context, rdb *redis.Client, instanceIds []string) (map[string]bool, error) {
	result := make(map[string]bool, len(instanceIds))
	if rdb == nil || len(instanceIds) == 0 {
		return result, nil
	}

	cmds := make(map[string]*redis.IntCmd, len(instanceIds))
	pipe := rdb.Pipeline()
	for _, instanceId := range instanceIds {
		key := fmt.Sprintf(constant.RedisKeyRouteInstance(), instanceId)
		cmds[instanceId] = pipe.Exists(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	for instanceId, cmd := range cmds {
		count, err := cmd.Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		result[instanceId] = count > 0
	}
	return result, nil
}

func collectInstanceIDs(routeMap map[string][]RouteConn) []string {
	uniq := make(map[string]struct{})
	for _, refs := range routeMap {
		for _, ref := range refs {
			uniq[ref.InstanceId] = struct{}{}
		}
	}

	instanceIds := make([]string, 0, len(uniq))
	for instanceId := range uniq {
		instanceIds = append(instanceIds, instanceId)
	}
	return instanceIds
}

func uniqueUserIDs(conns []RouteConn) map[string]struct{} {
	result := make(map[string]struct{}, len(conns))
	for _, conn := range conns {
		result[conn.UserId] = struct{}{}
	}
	return result
}

func formatRouteValue(conn RouteConn) string {
	return fmt.Sprintf("%s|%d", conn.InstanceId, conn.PlatformId)
}

func parseRouteValue(userId, connId, raw string) (RouteConn, bool) {
	parts := strings.Split(raw, "|")
	if len(parts) != 2 {
		return RouteConn{}, false
	}

	platformId := 0
	if _, err := fmt.Sscanf(parts[1], "%d", &platformId); err != nil {
		return RouteConn{}, false
	}

	return RouteConn{
		UserId:     userId,
		ConnId:     connId,
		InstanceId: parts[0],
		PlatformId: platformId,
	}, true
}

func formatInstanceRouteMember(conn RouteConn) string {
	return fmt.Sprintf("%s|%s", conn.UserId, conn.ConnId)
}

func parseInstanceRouteMember(member string) (string, string, bool) {
	parts := strings.Split(member, "|")
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func boolToRedis(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func redisBool(v string) bool {
	return v == "1"
}
