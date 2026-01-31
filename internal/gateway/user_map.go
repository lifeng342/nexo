package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mbeoliero/nexo/pkg/constant"
	"github.com/redis/go-redis/v9"
)

// UserMap manages user connections
type UserMap struct {
	mu    sync.RWMutex
	users map[string]*UserPlatform // userId -> UserPlatform
	rdb   *redis.Client
}

// UserPlatform holds all connections for a user
type UserPlatform struct {
	Clients []*Client
	Time    time.Time
}

// NewUserMap creates a new UserMap
func NewUserMap(rdb *redis.Client) *UserMap {
	return &UserMap{
		users: make(map[string]*UserPlatform),
		rdb:   rdb,
	}
}

// Register registers a client
func (m *UserMap) Register(ctx context.Context, client *Client) {
	m.mu.Lock()
	defer m.mu.Unlock()

	userPlatform, exists := m.users[client.UserId]
	if !exists {
		userPlatform = &UserPlatform{
			Clients: make([]*Client, 0, 4),
		}
		m.users[client.UserId] = userPlatform
	}

	userPlatform.Clients = append(userPlatform.Clients, client)
	userPlatform.Time = time.Now()

	// Update Redis online status
	m.setOnline(ctx, client.UserId)
}

// Unregister unregisters a client
func (m *UserMap) Unregister(ctx context.Context, client *Client) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	userPlatform, exists := m.users[client.UserId]
	if !exists {
		return false
	}

	// Remove the specific client
	newClients := make([]*Client, 0, len(userPlatform.Clients))
	for _, c := range userPlatform.Clients {
		if c.ConnId != client.ConnId {
			newClients = append(newClients, c)
		}
	}
	userPlatform.Clients = newClients

	// If no more clients, remove user from map
	if len(userPlatform.Clients) == 0 {
		delete(m.users, client.UserId)
		m.setOffline(ctx, client.UserId)
		return true // User completely disconnected
	}

	return false
}

// GetAll gets all clients for a user
func (m *UserMap) GetAll(userId string) ([]*Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userPlatform, exists := m.users[userId]
	if !exists {
		return nil, false
	}

	// Return a copy to avoid race conditions
	clients := make([]*Client, len(userPlatform.Clients))
	copy(clients, userPlatform.Clients)
	return clients, true
}

// GetByPlatform gets clients for a specific platform
func (m *UserMap) GetByPlatform(userId string, platformId int) ([]*Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userPlatform, exists := m.users[userId]
	if !exists {
		return nil, false
	}

	var clients []*Client
	for _, c := range userPlatform.Clients {
		if c.PlatformId == platformId {
			clients = append(clients, c)
		}
	}
	return clients, len(clients) > 0
}

// HasConnection checks if user has any connection
func (m *UserMap) HasConnection(userId string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userPlatform, exists := m.users[userId]
	return exists && len(userPlatform.Clients) > 0
}

// GetOnlineUserCount returns the number of online users
func (m *UserMap) GetOnlineUserCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.users)
}

// GetOnlineConnCount returns the total number of connections
func (m *UserMap) GetOnlineConnCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, up := range m.users {
		count += len(up.Clients)
	}
	return count
}

// IsOnline checks if user is online (checks Redis for distributed support)
func (m *UserMap) IsOnline(ctx context.Context, userId string) bool {
	// First check local
	if m.HasConnection(userId) {
		return true
	}

	// Then check Redis for multi-instance support
	if m.rdb != nil {
		key := fmt.Sprintf(constant.RedisKeyOnline(), userId)
		exists, _ := m.rdb.Exists(ctx, key).Result()
		return exists > 0
	}

	return false
}

// setOnline marks user as online in Redis
func (m *UserMap) setOnline(ctx context.Context, userId string) {
	if m.rdb == nil {
		return
	}

	key := fmt.Sprintf(constant.RedisKeyOnline(), userId)
	m.rdb.Set(ctx, key, "1", 60*time.Second)
}

// setOffline marks user as offline in Redis
func (m *UserMap) setOffline(ctx context.Context, userId string) {
	if m.rdb == nil {
		return
	}

	key := fmt.Sprintf(constant.RedisKeyOnline(), userId)
	m.rdb.Del(ctx, key)
}

// RefreshOnlineStatus refreshes the online status TTL
func (m *UserMap) RefreshOnlineStatus(ctx context.Context, userId string) {
	if m.rdb == nil {
		return
	}

	if m.HasConnection(userId) {
		key := fmt.Sprintf(constant.RedisKeyOnline(), userId)
		m.rdb.Expire(ctx, key, 60*time.Second)
	}
}

// GetAllOnlineUserIds returns all online user Ids (local only)
func (m *UserMap) GetAllOnlineUserIds() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userIds := make([]string, 0, len(m.users))
	for userId := range m.users {
		userIds = append(userIds, userId)
	}
	return userIds
}
