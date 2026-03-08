package gateway

import (
	"context"
	"sync"
	"time"
)

// UserMap manages user connections
type UserMap struct {
	mu    sync.RWMutex
	users map[string]*UserPlatform // userId -> UserPlatform
}

// UserPlatform holds all connections for a user
type UserPlatform struct {
	Clients []*Client
	Time    time.Time
}

// NewUserMap creates a new UserMap.
func NewUserMap() *UserMap {
	return &UserMap{
		users: make(map[string]*UserPlatform),
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

// IsOnline checks whether the user has at least one local connection.
func (m *UserMap) IsOnline(ctx context.Context, userId string) bool {
	return m.HasConnection(userId)
}

// RefreshOnlineStatus is now handled by OnlineStateWriter.
func (m *UserMap) RefreshOnlineStatus(ctx context.Context, userId string) {
	_ = ctx
	_ = userId
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

// SnapshotRouteConns returns the local connections as route DTOs.
func (m *UserMap) SnapshotRouteConns(instanceId string) []RouteConn {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conns := make([]RouteConn, 0, m.GetOnlineConnCount())
	for userId, userPlatform := range m.users {
		for _, client := range userPlatform.Clients {
			conns = append(conns, RouteConn{
				UserId:     userId,
				ConnId:     client.ConnId,
				InstanceId: instanceId,
				PlatformId: client.PlatformId,
			})
		}
	}
	return conns
}

// SnapshotClients returns a flat copy of all local clients.
func (m *UserMap) SnapshotClients() []*Client {
	m.mu.RLock()
	defer m.mu.RUnlock()

	clients := make([]*Client, 0, m.GetOnlineConnCount())
	for _, userPlatform := range m.users {
		clients = append(clients, userPlatform.Clients...)
	}
	return clients
}
