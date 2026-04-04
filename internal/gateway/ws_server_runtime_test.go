package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mbeoliero/kit/log"
	"github.com/mbeoliero/nexo/internal/cluster"
	"github.com/mbeoliero/nexo/internal/config"
)

func captureGatewayLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	log.SetOutput(buf)
	t.Cleanup(func() {
		log.SetOutput(io.Discard)
	})
	return buf
}

func TestUnregisterClientDoesNotDropCleanupWhenQueueIsFullDuringDrain(t *testing.T) {
	server := &WsServer{
		cfg:            &config.Config{},
		userMap:        NewUserMap(nil),
		unregisterChan: make(chan *Client, 1),
	}
	server.unregisterChan <- &Client{UserId: "other", ConnId: "other"}

	client := &Client{UserId: "u1", PlatformId: 5, ConnId: "c1"}
	server.userMap.Register(context.Background(), client)
	server.onlineUserNum.Store(1)
	server.onlineConnNum.Store(1)

	server.UnregisterClient(client)

	if got := server.onlineConnNum.Load(); got != 0 {
		t.Fatalf("online conn count mismatch: got %d want 0", got)
	}
	if got := server.onlineUserNum.Load(); got != 0 {
		t.Fatalf("online user count mismatch: got %d want 0", got)
	}
	if _, ok := server.userMap.GetAll("u1"); ok {
		t.Fatal("client cleanup was dropped")
	}
}

func TestUnregisterClientAccountingIsIdempotent(t *testing.T) {
	server := &WsServer{
		cfg:     &config.Config{},
		userMap: NewUserMap(nil),
	}

	client := &Client{UserId: "u1", PlatformId: 5, ConnId: "c1"}
	server.userMap.Register(context.Background(), client)
	server.onlineUserNum.Store(1)
	server.onlineConnNum.Store(1)

	server.unregisterClient(context.Background(), client)
	server.unregisterClient(context.Background(), client)

	if got := server.onlineConnNum.Load(); got != 0 {
		t.Fatalf("online conn count mismatch after duplicate unregister: got %d want 0", got)
	}
	if got := server.onlineUserNum.Load(); got != 0 {
		t.Fatalf("online user count mismatch after duplicate unregister: got %d want 0", got)
	}
}

func TestLocalConnSnapshotMatchesRegisteredClients(t *testing.T) {
	server := &WsServer{
		userMap: NewUserMap(nil),
	}
	server.userMap.Register(context.Background(), &Client{UserId: "u1", PlatformId: 5, ConnId: "c1"})
	server.userMap.Register(context.Background(), &Client{UserId: "u2", PlatformId: 1, ConnId: "c2"})

	got := server.LocalRouteSnapshot("i1")
	if len(got) != 2 {
		t.Fatalf("snapshot len mismatch: got %d", len(got))
	}
}

type stubRouteMirrorStore struct {
	registerErr   error
	unregisterErr error
}

func (s *stubRouteMirrorStore) RegisterConn(context.Context, cluster.RouteConn) error {
	return s.registerErr
}

func (s *stubRouteMirrorStore) UnregisterConn(context.Context, cluster.RouteConn) error {
	return s.unregisterErr
}

type blockingRouteMirrorStore struct {
	registerStarted   chan struct{}
	unregisterStarted chan struct{}
	release           chan struct{}
}

func (s *blockingRouteMirrorStore) RegisterConn(context.Context, cluster.RouteConn) error {
	select {
	case s.registerStarted <- struct{}{}:
	default:
	}
	<-s.release
	return nil
}

func (s *blockingRouteMirrorStore) UnregisterConn(context.Context, cluster.RouteConn) error {
	select {
	case s.unregisterStarted <- struct{}{}:
	default:
	}
	<-s.release
	return nil
}

func TestRouteMirrorFailuresIncrementRuntimeStats(t *testing.T) {
	server := &WsServer{
		cfg:          &config.Config{},
		userMap:      NewUserMap(nil),
		routeStore:   &stubRouteMirrorStore{registerErr: errors.New("register failed"), unregisterErr: errors.New("unregister failed")},
		instanceId:   "i1",
		runtimeStats: cluster.NewRuntimeStats(),
	}

	client := &Client{UserId: "u1", PlatformId: 5, ConnId: "c1"}

	server.registerClient(context.Background(), client)
	server.unregisterClient(context.Background(), client)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := server.runtimeStats.Snapshot().RouteMirrorErrorsTotal; got == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("route mirror error count mismatch: got %d want 2", server.runtimeStats.Snapshot().RouteMirrorErrorsTotal)
}

func TestRegisterClientDoesNotBlockWhenRouteMirrorStoreIsSlow(t *testing.T) {
	store := &blockingRouteMirrorStore{
		registerStarted:   make(chan struct{}, 1),
		unregisterStarted: make(chan struct{}, 1),
		release:           make(chan struct{}),
	}
	server := &WsServer{
		cfg:             &config.Config{},
		userMap:         NewUserMap(nil),
		routeStore:      store,
		instanceId:      "i1",
		routeMirrorChan: make(chan routeMirrorTask, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.routeMirrorLoop(ctx)

	doneFirst := make(chan struct{})
	go func() {
		server.registerClient(context.Background(), &Client{UserId: "u1", PlatformId: 5, ConnId: "c1"})
		close(doneFirst)
	}()

	select {
	case <-doneFirst:
	case <-time.After(time.Second):
		t.Fatal("first registerClient blocked on route mirror")
	}

	select {
	case <-store.registerStarted:
	case <-time.After(time.Second):
		t.Fatal("expected route mirror worker to start")
	}

	doneSecond := make(chan struct{})
	go func() {
		server.registerClient(context.Background(), &Client{UserId: "u2", PlatformId: 1, ConnId: "c2"})
		close(doneSecond)
	}()

	select {
	case <-doneSecond:
	case <-time.After(time.Second):
		t.Fatal("second registerClient blocked while route mirror worker was stalled")
	}

	if got := server.onlineConnNum.Load(); got != 2 {
		t.Fatalf("online conn count mismatch: got %d want 2", got)
	}

	close(store.release)
}

func TestEnqueueRouteMirrorInvokesOverflowHandlerWhenQueueIsFull(t *testing.T) {
	logBuf := captureGatewayLogs(t)
	server := &WsServer{
		cfg:             &config.Config{},
		userMap:         NewUserMap(nil),
		routeStore:      &stubRouteMirrorStore{},
		instanceId:      "i1",
		routeMirrorChan: make(chan routeMirrorTask, 1),
		runtimeStats:    cluster.NewRuntimeStats(),
	}
	server.routeMirrorChan <- routeMirrorTask{}

	overflow := make(chan struct{}, 1)
	server.SetRouteMirrorOverflowHandler(func() {
		overflow <- struct{}{}
	})

	server.enqueueRouteMirror(context.Background(), &Client{UserId: "u1", PlatformId: 5, ConnId: "c1"}, true)

	select {
	case <-overflow:
	case <-time.After(time.Second):
		t.Fatal("expected route mirror overflow handler to be invoked")
	}

	if got := server.runtimeStats.Snapshot().RouteMirrorErrorsTotal; got != 1 {
		t.Fatalf("route mirror error count mismatch: got %d want 1", got)
	}
	if !strings.Contains(logBuf.String(), "route mirror queue full") || !strings.Contains(logBuf.String(), "queue_depth=1") {
		t.Fatalf("expected overflow log with queue depth, got %q", logBuf.String())
	}
}

func TestEnqueueRouteMirrorInvokesOverflowHandlerOnlyOncePerOverloadWindow(t *testing.T) {
	server := &WsServer{
		cfg:             &config.Config{},
		userMap:         NewUserMap(nil),
		routeStore:      &stubRouteMirrorStore{},
		instanceId:      "i1",
		routeMirrorChan: make(chan routeMirrorTask, 1),
		runtimeStats:    cluster.NewRuntimeStats(),
	}
	server.routeMirrorChan <- routeMirrorTask{}

	var calls atomic.Int32
	server.SetRouteMirrorOverflowHandler(func() {
		calls.Add(1)
	})

	client := &Client{UserId: "u1", PlatformId: 5, ConnId: "c1"}
	server.enqueueRouteMirror(context.Background(), client, true)
	server.enqueueRouteMirror(context.Background(), client, false)

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && calls.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("overflow handler call count mismatch: got %d want 1", got)
	}
}

func TestRegisterAndUnregisterClientKeepSuccessLogsBelowInfo(t *testing.T) {
	oldLogLevel := log.GetLogLevel()
	log.SetLevel(log.LevelInfo)
	defer log.SetLevel(oldLogLevel)

	logBuf := captureGatewayLogs(t)
	server := &WsServer{
		cfg:     &config.Config{},
		userMap: NewUserMap(nil),
	}

	client := &Client{UserId: "u1", PlatformId: 5, ConnId: "c1"}
	server.registerClient(context.Background(), client)
	server.unregisterClient(context.Background(), client)

	if got := server.onlineConnNum.Load(); got != 0 {
		t.Fatalf("online conn count mismatch: got %d want 0", got)
	}
	if got := server.onlineUserNum.Load(); got != 0 {
		t.Fatalf("online user count mismatch: got %d want 0", got)
	}
	logs := logBuf.String()
	if strings.Contains(logs, "client registered") {
		t.Fatalf("register success log should stay below info level, got %q", logs)
	}
	if strings.Contains(logs, "client unregistered") {
		t.Fatalf("unregister success log should stay below info level, got %q", logs)
	}
}
