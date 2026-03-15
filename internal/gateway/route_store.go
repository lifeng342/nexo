package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mbeoliero/nexo/pkg/constant"
	"github.com/redis/go-redis/v9"
)

type RouteConn struct {
	UserId     string
	ConnId     string
	InstanceId string
	PlatformId int
}

type RouteStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewRouteStore(rdb *redis.Client, ttl time.Duration) *RouteStore {
	return &RouteStore{rdb: rdb, ttl: ttl}
}

func routeUserKey(userID string) string { return constant.GetRedisKeyPrefix() + "route:user:" + userID }
func routeInstKey(instanceID string) string { return constant.GetRedisKeyPrefix() + "route:inst:" + instanceID }
func routeInstMember(userID, connID string) string { return userID + "|" + connID }
func routeValue(instanceID string, platformID int) string { return fmt.Sprintf("%s|%d", instanceID, platformID) }

func parseRouteValue(userID, connID, value string) RouteConn {
	parts := strings.SplitN(value, "|", 2)
	conn := RouteConn{UserId: userID, ConnId: connID}
	if len(parts) > 0 {
		conn.InstanceId = parts[0]
	}
	if len(parts) > 1 {
		conn.PlatformId = parseRedisInt(parts[1])
	}
	return conn
}

func (s *RouteStore) RegisterConn(ctx context.Context, conn RouteConn) error {
	pipe := s.rdb.TxPipeline()
	pipe.HSet(ctx, routeUserKey(conn.UserId), conn.ConnId, routeValue(conn.InstanceId, conn.PlatformId))
	pipe.Expire(ctx, routeUserKey(conn.UserId), s.ttl)
	pipe.SAdd(ctx, routeInstKey(conn.InstanceId), routeInstMember(conn.UserId, conn.ConnId))
	pipe.Expire(ctx, routeInstKey(conn.InstanceId), s.ttl)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RouteStore) UnregisterConn(ctx context.Context, conn RouteConn) error {
	pipe := s.rdb.TxPipeline()
	pipe.HDel(ctx, routeUserKey(conn.UserId), conn.ConnId)
	pipe.SRem(ctx, routeInstKey(conn.InstanceId), routeInstMember(conn.UserId, conn.ConnId))
	_, err := pipe.Exec(ctx)
	return err
}

func (s *RouteStore) GetUsersConnRefs(ctx context.Context, userIDs []string) (map[string][]RouteConn, error) {
	return s.getUsersConnRefs(ctx, userIDs, true)
}

func (s *RouteStore) GetUsersPresenceConnRefs(ctx context.Context, userIDs []string) (map[string][]RouteConn, error) {
	return s.getUsersConnRefs(ctx, userIDs, false)
}

func (s *RouteStore) getUsersConnRefs(ctx context.Context, userIDs []string, requireRouteable bool) (map[string][]RouteConn, error) {
	result := make(map[string][]RouteConn, len(userIDs))
	for _, userID := range userIDs {
		entries, err := s.rdb.HGetAll(ctx, routeUserKey(userID)).Result()
		if err != nil {
			return nil, err
		}
		refs := make([]RouteConn, 0, len(entries))
		for connID, value := range entries {
			conn := parseRouteValue(userID, connID, value)
			state, err := s.rdb.HGetAll(ctx, instanceAliveKey(conn.InstanceId)).Result()
			if err != nil {
				return nil, err
			}
			if len(state) == 0 {
				continue
			}
			if requireRouteable && !redisToBool(state["routeable"]) {
				continue
			}
			refs = append(refs, conn)
		}
		result[userID] = refs
	}
	return result, nil
}

func (s *RouteStore) ReconcileInstanceRoutes(ctx context.Context, instanceID string, conns []RouteConn, staleCleanupLimit int) error {
	expected := make(map[string]RouteConn, len(conns))
	for _, conn := range conns {
		expected[routeInstMember(conn.UserId, conn.ConnId)] = conn
		if err := s.RegisterConn(ctx, conn); err != nil {
			return err
		}
	}

	members, err := s.rdb.SMembers(ctx, routeInstKey(instanceID)).Result()
	if err != nil {
		return err
	}
	cleaned := 0
	for _, member := range members {
		if _, ok := expected[member]; ok {
			continue
		}
		if staleCleanupLimit > 0 && cleaned >= staleCleanupLimit {
			break
		}
		parts := strings.SplitN(member, "|", 2)
		if len(parts) != 2 {
			continue
		}
		stale := RouteConn{UserId: parts[0], ConnId: parts[1], InstanceId: instanceID}
		if err := s.UnregisterConn(ctx, stale); err != nil {
			return err
		}
		cleaned++
	}
	return nil
}
