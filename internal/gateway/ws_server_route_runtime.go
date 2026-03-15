package gateway

import (
	"context"
	"time"

	"github.com/mbeoliero/kit/log"
)

func (s *WsServer) initRouteRuntime() {
	if s == nil || s.cfg == nil || !s.cfg.WebSocket.CrossInstance.Enabled {
		return
	}
	if s.routeWriteChan == nil {
		size := s.cfg.WebSocket.CrossInstance.RouteWriteQueueSize
		if size <= 0 {
			size = 1000
		}
		s.routeWriteChan = make(chan routeWriteEvent, size)
	}
	if s.routeConnState == nil {
		s.routeConnState = make(map[routeConnKey]routeConnState)
	}
}

func routeConnDescriptorFromClient(client *Client, instanceID string) routeConnDescriptor {
	return routeConnDescriptor{UserID: client.UserId, ConnID: client.ConnId, PlatformID: client.PlatformId, InstanceID: instanceID}
}

func (s *WsServer) markRouteConnDesired(desc routeConnDescriptor, desired routeDesiredState) {
	if s == nil {
		return
	}
	key := routeConnKey{UserID: desc.UserID, ConnID: desc.ConnID}
	s.routeStateMu.Lock()
	defer s.routeStateMu.Unlock()
	s.routeConnState[key] = routeConnState{Descriptor: desc, Desired: desired}
}

func (s *WsServer) getRouteConnState(key routeConnKey) (routeConnState, bool) {
	s.routeStateMu.Lock()
	defer s.routeStateMu.Unlock()
	state, ok := s.routeConnState[key]
	return state, ok
}

func (s *WsServer) clearRouteConnState(key routeConnKey) {
	s.routeStateMu.Lock()
	defer s.routeStateMu.Unlock()
	delete(s.routeConnState, key)
}

func (s *WsServer) enqueueRouteWrite(desc routeConnDescriptor, desired routeDesiredState) {
	s.markRouteConnDesired(desc, desired)
	if s.routeWriteChan == nil {
		return
	}
	event := routeWriteEvent{Descriptor: desc, Desired: desired}
	select {
	case s.routeWriteChan <- event:
	default:
	}
}

func (s *WsServer) handleRouteWriteEvent(ctx context.Context, event routeWriteEvent) {
	key := routeConnKey{UserID: event.Descriptor.UserID, ConnID: event.Descriptor.ConnID}
	state, ok := s.getRouteConnState(key)
	if !ok {
		return
	}
	desired := state.Desired
	if desired != event.Desired {
		if desired == routeDesiredUnregistered {
			event.Desired = routeDesiredUnregistered
		} else {
			return
		}
	}
	if s.routeRecorder != nil {
		op := "register"
		if event.Desired == routeDesiredUnregistered {
			op = "unregister"
		}
		s.routeRecorder.events = append(s.routeRecorder.events, op+":"+event.Descriptor.UserID+":"+event.Descriptor.ConnID)
	}
	writeSucceeded := false
	if s.routeStore != nil {
		conn := RouteConn{UserId: event.Descriptor.UserID, ConnId: event.Descriptor.ConnID, InstanceId: event.Descriptor.InstanceID, PlatformId: event.Descriptor.PlatformID}
		var err error
		if event.Desired == routeDesiredRegistered {
			err = s.routeStore.RegisterConn(ctx, conn)
		} else {
			err = s.routeStore.UnregisterConn(ctx, conn)
		}
		if err != nil {
			return
		}
		writeSucceeded = true
	} else if event.Desired == routeDesiredUnregistered {
		writeSucceeded = true
	}
	state, ok = s.getRouteConnState(key)
	if !ok || state.Desired != desired || !writeSucceeded {
		return
	}
	s.clearRouteConnState(key)
}

func (s *WsServer) hasLocalConn(userID, connID string) bool {
	clients, ok := s.userMap.GetAll(userID)
	if !ok {
		return false
	}
	for _, client := range clients {
		if client.ConnId == connID {
			return true
		}
	}
	return false
}

func (s *WsServer) runRouteRepairOnce(ctx context.Context) {
	s.routeStateMu.Lock()
	states := make([]routeConnState, 0, len(s.routeConnState))
	for _, state := range s.routeConnState {
		states = append(states, state)
	}
	s.routeStateMu.Unlock()

	for _, state := range states {
		s.handleRouteWriteEvent(ctx, routeWriteEvent{Descriptor: state.Descriptor, Desired: state.Desired})
	}
}

func (s *WsServer) tryEnqueueRegister(ctx context.Context, client *Client) error {
	if s == nil {
		return context.Canceled
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.registerChan <- client:
		return nil
	default:
		return context.DeadlineExceeded
	}
}

func (s *WsServer) routeWriteLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.routeWriteChan:
			s.handleRouteWriteEvent(ctx, event)
		}
	}
}

func (s *WsServer) routeRepairLoop(ctx context.Context) {
	interval := time.Duration(s.cfg.WebSocket.CrossInstance.RouteRepairIntervalMs) * time.Millisecond
	if interval <= 0 {
		interval = time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			s.runRouteRepairOnce(ctx)
		}
	}
}

func (s *WsServer) routeReconcileLoop(ctx context.Context) {
	if s.routeStore == nil || s.instanceID == "" || s.cfg == nil {
		return
	}
	interval := time.Duration(s.cfg.WebSocket.CrossInstance.RouteReconcileIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	for {
		if err := s.routeStore.ReconcileInstanceRoutes(ctx, s.instanceID, s.SnapshotRouteConns(s.instanceID), s.cfg.WebSocket.CrossInstance.RouteStaleCleanupLimit); err != nil {
			log.CtxWarn(ctx, "route reconcile failed: instance_id=%s, error=%v", s.instanceID, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}
