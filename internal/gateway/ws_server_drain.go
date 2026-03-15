package gateway

import (
	"context"
	"time"
)

func (s *WsServer) SetDrainUnregisterOnKick(enabled bool) {
	s.drainAutoUnregisterOnKick = enabled
}

func (s *WsServer) DrainLocalClients(ctx context.Context, opts DrainLocalClientsOptions) DrainLocalClientsResult {
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1
	}
	s.drainPendingRegistrations(ctx)
	clients := s.snapshotAllClients()
	result := DrainLocalClientsResult{}
	for i := 0; i < len(clients); i += opts.BatchSize {
		end := i + opts.BatchSize
		if end > len(clients) {
			end = len(clients)
		}
		for _, client := range clients[i:end] {
			result.KickAttempts++
			if err := client.KickOnline(); err != nil {
				result.KickFailures++
				result.FallbackCloses++
				_ = client.Close()
			}
			if s.drainAutoUnregisterOnKick {
				s.unregisterClient(context.Background(), client)
			}
		}
		if end < len(clients) && opts.BatchInterval > 0 {
			select {
			case <-ctx.Done():
				end = len(clients)
			case <-time.After(opts.BatchInterval):
			}
		}
	}

	deadline := time.Now().Add(opts.DrainTimeout)
	drainInterrupted := false
	for s.GetOnlineConnCount() > 0 && opts.DrainTimeout > 0 && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			drainInterrupted = true
		case <-time.After(time.Millisecond):
		}
		if drainInterrupted {
			break
		}
	}
	if s.GetOnlineConnCount() > 0 {
		survivors := s.snapshotAllClients()
		result.Survivors = len(survivors)
		for _, client := range survivors {
			result.FallbackCloses++
			_ = client.Close()
			s.unregisterClient(context.Background(), client)
		}
	}
	if opts.SettleTimeout > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(opts.SettleTimeout):
		}
	}
	return result
}

func (s *WsServer) snapshotAllClients() []*Client {
	userIDs := s.GetAllOnlineUserIds()
	clients := make([]*Client, 0)
	for _, userID := range userIDs {
		userClients, ok := s.GetAllClients(userID)
		if !ok {
			continue
		}
		clients = append(clients, userClients...)
	}
	return clients
}

func (s *WsServer) drainPendingRegistrations(ctx context.Context) {
	for {
		select {
		case client := <-s.registerChan:
			s.registerClient(ctx, client)
		default:
			return
		}
	}
}

func newDrainTestServer(connCount int) *WsServer {
	server := &WsServer{userMap: NewUserMap(nil), drainAutoUnregisterOnKick: true}
	for i := 0; i < connCount; i++ {
		client := NewClient(&drainTestConn{}, "u1", i+1, "", "", string(rune('a'+i)), server)
		server.userMap.Register(context.Background(), client)
		server.onlineConnNum.Add(1)
	}
	server.onlineUserNum.Store(1)
	return server
}

type drainTestConn struct{}

func (d *drainTestConn) ReadMessage() ([]byte, error) { return nil, context.Canceled }
func (d *drainTestConn) WriteMessage(data []byte) error { return nil }
func (d *drainTestConn) WriteControlMessage(data []byte) error { return nil }
func (d *drainTestConn) Close() error { return nil }
func (d *drainTestConn) SetReadDeadline(t time.Time) error { return nil }
func (d *drainTestConn) SetWriteDeadline(t time.Time) error { return nil }
