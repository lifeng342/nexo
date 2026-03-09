# Route-Only Multi-Instance Push Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the phase-1 multi-instance push foundation for nexo_v2 with lifecycle gating, route-only cross-instance delivery, and presence aggregation without introducing broadcast/hybrid behavior.

**Architecture:** Keep `MessageService` as the write-path entry and add a scoped cross-instance layer around the gateway. In phase 1, `WsServer` remains the existing gateway runtime shell that still owns connection ingress, event loops, and local delivery, while `LifecycleGate`, `PresenceService`, `PushCoordinator`, `RouteStore`, and `InstanceManager` are introduced around it to peel off shutdown gating, presence aggregation, and cross-instance route delivery incrementally. Phase 1 must prefer correctness over optimization: route-only, no broadcast fallback, no broad capability control plane, and no large-scale rewrite of the existing gateway runtime.

**Tech Stack:** Go, Hertz, gorilla/websocket, Redis, MySQL, existing `internal/service`, `internal/gateway`, `pkg/response`, `pkg/errcode`

---

### Task 1: Add lifecycle gate and shutdown error primitives

**Files:**
- Create: `internal/gateway/lifecycle_gate.go`
- Modify: `pkg/errcode/errcode.go:31-78`
- Modify: `pkg/response/response.go:13-75`
- Test: `internal/gateway/lifecycle_gate_test.go`
- Test: `pkg/response/response_test.go`

**Step 1: Write the failing tests**

Create `internal/gateway/lifecycle_gate_test.go` with table-driven tests for:

```go
func TestLifecycleGateAcquireSendLease(t *testing.T) {
    gate := NewLifecycleGate()
    release, err := gate.AcquireSendLease()
    require.NoError(t, err)
    require.NotNil(t, release)
    release()
}

func TestLifecycleGateBeginSendDrainRejectsNewLease(t *testing.T) {
    gate := NewLifecycleGate()
    gate.BeginSendDrain()
    _, err := gate.AcquireSendLease()
    require.ErrorIs(t, err, errcode.ErrServerShuttingDown)
}

func TestLifecycleGateCloseSendPathAfterInflightDrains(t *testing.T) {
    gate := NewLifecycleGate()
    release, err := gate.AcquireSendLease()
    require.NoError(t, err)
    gate.BeginSendDrain()
    require.False(t, gate.Snapshot().SendClosed)
    release()
    gate.CloseSendPath()
    require.True(t, gate.Snapshot().SendClosed)
}
```

Create `pkg/response/response_test.go` with tests for a new response helper:

```go
func TestErrorUses503ForServerShuttingDown(t *testing.T) {
    // Build RequestContext, call response.Error with errcode.ErrServerShuttingDown,
    // assert HTTP status == 503 and response body code matches errcode.
}

func TestSuccessWithMeta(t *testing.T) {
    // Assert JSON includes code/message/data/meta.
}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/gateway ./pkg/response
```

Expected:
- FAIL because `NewLifecycleGate`, `LifecycleSnapshot`, `SuccessWithMeta`, and `ErrServerShuttingDown` do not exist yet.

**Step 3: Write minimal implementation**

Create `internal/gateway/lifecycle_gate.go` with:

```go
type LifecycleSnapshot struct {
    Ready         bool
    Routeable     bool
    IngressClosed bool
    SendDraining  bool
    SendClosed    bool
    Draining      bool
    InflightSend  int64
}

type DrainReason string

const (
    DrainReasonPlannedUpgrade DrainReason = "planned_upgrade"
    DrainReasonSubscribeFault DrainReason = "subscribe_fault"
    DrainReasonManual         DrainReason = "manual"
)

type LifecycleGate interface {
    CanAcceptIngress() bool
    CanStartSend() bool
    AcquireSendLease() (release func(), err error)
    IsReady() bool
    IsRouteable() bool
    Snapshot() LifecycleSnapshot
    MarkUnready()
    MarkUnrouteable()
    CloseIngress()
    BeginSendDrain()
    CloseSendPath()
    EnterDraining()
}
```

Implement a concrete gate with:
- default `ready=true`, `routeable=true`
- atomic/mutex-protected fields
- inflight send counter
- `AcquireSendLease()` returning `errcode.ErrServerShuttingDown`
- `CloseSendPath()` returning immediately if inflight > 0 only in tests by calling it after release

In `pkg/errcode/errcode.go`, add:

```go
ErrServerShuttingDown = New(1008, "server shutting down")
```

In `pkg/response/response.go`, add:

```go
type Meta map[string]any

type Response struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
    Data    any    `json:"data,omitempty"`
    Meta    Meta   `json:"meta,omitempty"`
}

func SuccessWithMeta(ctx context.Context, c *app.RequestContext, data any, meta Meta) {
    c.JSON(http.StatusOK, Response{Code: 0, Message: "success", Data: data, Meta: meta})
}
```

Update `Error` / `ErrorWithCode` so `ErrServerShuttingDown` maps to `http.StatusServiceUnavailable`.

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/gateway ./pkg/response
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/gateway/lifecycle_gate.go internal/gateway/lifecycle_gate_test.go pkg/errcode/errcode.go pkg/response/response.go pkg/response/response_test.go
git commit -m "feat: add lifecycle gate and shutdown response primitives"
```

---

### Task 2: Wire lifecycle gating into HTTP send, WS send, WS ingress, and readiness endpoint

**Files:**
- Modify: `internal/handler/message_handler.go:15-46`
- Modify: `internal/gateway/ws_server.go:184-397`
- Modify: `internal/router/router.go:17-77`
- Test: `internal/handler/message_handler_test.go`
- Test: `internal/gateway/ws_server_test.go` or equivalent targeted lifecycle test
- Test: `internal/router/router_test.go`

**Step 1: Write the failing tests**

Create `internal/handler/message_handler_test.go` with a stub gate and stub message service wrapper:

```go
func TestSendMessageRejectsWhenGateClosed(t *testing.T) {
    gate := &stubLifecycleGate{acquireErr: errcode.ErrServerShuttingDown}
    handler := NewMessageHandler(stubMsgService, gate)
    // Build Hertz context with authenticated user and valid request.
    // Expect HTTP 503 and code == ErrServerShuttingDown.Code.
}

func TestSendMessageReleasesLeaseAfterServiceCall(t *testing.T) {
    gate := &countingGate{}
    handler := NewMessageHandler(stubMsgService, gate)
    // Expect acquire called once and release called once.
}
```

Create `internal/router/router_test.go` with a minimal server or route registration check:

```go
func TestReadyEndpointReturns503WhenGateNotReady(t *testing.T) {
    // Register router with a stub readiness source.
    // GET /ready should return 503 when IsReady() == false.
}
```

Create `internal/gateway/ws_server_test.go` or equivalent targeted lifecycle tests for:

```go
func TestHandleConnectionRejectsWhenIngressClosed(t *testing.T) {}
func TestHandleSendMsgRejectsWhenSendLeaseUnavailable(t *testing.T) {}
func TestAsyncPushToUsersDropsOrRejectsWhenSendPathClosed(t *testing.T) {}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/handler ./internal/gateway ./internal/router
```

Expected:
- FAIL because `MessageHandler` does not accept a gate, WS send/ingress are not lifecycle-aware, and `/ready` does not exist.

**Step 3: Write minimal implementation**

In `internal/handler/message_handler.go`:
- add a small interface dependency instead of a full `WsServer` reference:

```go
type SendGate interface {
    AcquireSendLease() (func(), error)
}
```

- update handler struct:

```go
type MessageHandler struct {
    msgService *service.MessageService
    gate       SendGate
}
```

- update constructor:

```go
func NewMessageHandler(msgService *service.MessageService, gate SendGate) *MessageHandler
```

- at the start of `SendMessage`, before `msgService.SendMessage(...)`:

```go
release, err := h.gate.AcquireSendLease()
if err != nil {
    response.Error(ctx, c, err)
    return
}
defer release()
```

In `internal/gateway/ws_server.go`:
- add a small lifecycle dependency usable by both WS handshake and `HandleSendMsg`
- reject new `/ws` upgrades when ingress is closed / not ready
- acquire `SendLease` before entering `MessageService.SendMessage(...)` inside `HandleSendMsg`
- ensure `AsyncPushToUsers(...)` treats closed send path as a hard stop or explicit drop with shutdown-aware logging rather than silent normal-path behavior

In `internal/router/router.go`:
- add a minimal readiness dependency, preferably reusing the gate via a small interface:

```go
type Readiness interface { IsReady() bool }
```

- add:

```go
h.GET("/ready", func(ctx context.Context, c *app.RequestContext) {
    if !gate.IsReady() {
        c.JSON(consts.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
        return
    }
    c.JSON(consts.StatusOK, map[string]string{"status": "ready"})
})
```

Pass the readiness dependency into `SetupRouter(...)`.

This task is only complete when the same lifecycle semantics cover:
- HTTP `/msg/send`
- WS `WSSendMsg`
- WS handshake `/ws`
- `AsyncPushToUsers(...)` as secondary protection

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/handler ./internal/gateway ./internal/router
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/handler/message_handler.go internal/handler/message_handler_test.go internal/gateway/ws_server.go internal/gateway/ws_server_test.go internal/router/router.go internal/router/router_test.go
git commit -m "feat: gate send paths and add readiness endpoint"
```

---

### Task 3: Make websocket connection config real and prepare kick-control semantics

**Files:**
- Modify: `internal/gateway/client_conn.go:20-143`
- Modify: `internal/gateway/ws_server.go:184-235`
- Modify: `internal/gateway/client.go:144-208`
- Test: `internal/gateway/client_conn_test.go`
- Test: `internal/gateway/client_test.go`

**Step 1: Write the failing tests**

Create `internal/gateway/client_conn_test.go`:

```go
func TestNewWebSocketClientConnUsesConfiguredWriteChannelSize(t *testing.T) {
    conn := NewWebSocketClientConn(fakeConn, 1024, time.Second, time.Second, time.Second, 8)
    require.Equal(t, 8, cap(conn.writeChan))
}

func TestWriteMessageReturnsErrWriteChannelFullWhenBufferIsFull(t *testing.T) {
    conn := &WebsocketClientConn{writeChan: make(chan []byte, 1)}
    require.NoError(t, conn.WriteMessage([]byte("a")))
    require.ErrorIs(t, conn.WriteMessage([]byte("b")), ErrWriteChannelFull)
}
```

Create `internal/gateway/client_test.go`:

```go
func TestKickOnlinePrefersControlWriteBeforeClose(t *testing.T) {
    // Use a fake ClientConn that records writes and closes.
    // Expect kick response is written before Close.
}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/gateway
```

Expected:
- FAIL because constructor signature and kick semantics do not match.

**Step 3: Write minimal implementation**

In `internal/gateway/client_conn.go`:
- expand constructor signature:

```go
func NewWebSocketClientConn(conn *websocket.Conn, maxMsgSize int64, writeWait, pongWait, pingPeriod time.Duration, writeChannelSize int) *WebsocketClientConn
```

- use `writeChannelSize` instead of hard-coded `256`
- use provided `writeWait` instead of `WriteWait` constant

In `internal/gateway/ws_server.go:225-229`, update call site to pass:
- `s.cfg.WebSocket.WriteWait`
- `s.cfg.WebSocket.PongWait`
- `s.cfg.WebSocket.PingPeriod`
- `s.cfg.WebSocket.WriteChannelSize`

In `internal/gateway/client.go`, minimally improve `KickOnline()`:
- keep it synchronous under the client mutex path
- attempt control response write
- only then close
- preserve current behavior if write fails, but ensure tests prove write is attempted before close

Do not implement a full priority queue yet; this task is only to align code with config and make the control-path ordering explicit.

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/gateway
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/gateway/client_conn.go internal/gateway/client_conn_test.go internal/gateway/client.go internal/gateway/client_test.go internal/gateway/ws_server.go
git commit -m "fix: apply websocket config and harden kick ordering"
```

---

### Task 4: Add presence service and move user online status off WsServer

**Files:**
- Create: `internal/service/presence_service.go`
- Modify: `internal/handler/user_handler.go:14-122`
- Modify: `internal/gateway/ws_server.go` (remove direct handler-facing online aggregation if obsolete)
- Test: `internal/service/presence_service_test.go`
- Test: `internal/handler/user_handler_test.go`

**Step 1: Write the failing tests**

Create `internal/service/presence_service_test.go`:

```go
func TestGetUsersOnlineStatusMergesLocalAndRemotePresence(t *testing.T) {}
func TestGetUsersOnlineStatusReturnsPartialMetaWhenRemoteFails(t *testing.T) {}
func TestGetUsersOnlineStatusDoesNotTreatRouteableFalseAsOffline(t *testing.T) {}
func TestGetUsersOnlineStatusKeepsPlatformDetailShape(t *testing.T) {}
```

Create `internal/handler/user_handler_test.go`:

```go
func TestGetUsersOnlineStatusRejectsMoreThan100UserIDs(t *testing.T) {}
func TestGetUsersOnlineStatusReturnsSuccessWithMetaOnPartial(t *testing.T) {}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/service ./internal/handler
```

Expected:
- FAIL because `PresenceService` does not exist and handler still depends on `WsServer`.

**Step 3: Write minimal implementation**

Create `internal/service/presence_service.go`:

```go
type OnlineStatusResult struct {
    UserId               string
    Status               int
    DetailPlatformStatus []PlatformStatusDetail
}

type PlatformStatusDetail struct {
    PlatformId   int
    PlatformName string
    ConnId       string
}

type PresenceService struct {
    userMap    localPresenceReader
    routeStore remotePresenceReader
    instanceID string
}

func NewPresenceService(...) *PresenceService
func (s *PresenceService) GetUsersOnlineStatus(ctx context.Context, userIDs []string) ([]*OnlineStatusResult, response.Meta, error)
```

Behavior:
- read local connections from `UserMap`
- read remote presence from `RouteStore.GetUsersPresenceConnRefs(...)`
- exclude self instance from remote merge
- keep response shape compatible with the current online-status contract, including platform detail fields
- define the DTO outside `gateway/ws_server.go` so `service` does not depend on gateway internals
- if remote fails, return local results plus `response.Meta{"partial": true, "data_source": "local_only"}` and no hard failure

Update `internal/handler/user_handler.go`:
- replace `wsServer *gateway.WsServer` dependency with a small presence interface
- strengthen request validation to `vd:"len($)>0,len($)<=100"`
- call `response.SuccessWithMeta(...)` when meta is non-empty, otherwise `response.Success(...)`

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/service ./internal/handler
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/service/presence_service.go internal/service/presence_service_test.go internal/handler/user_handler.go internal/handler/user_handler_test.go internal/gateway/ws_server.go
git commit -m "feat: add presence service and aggregated online status"
```

---

### Task 5: Add route store primitives and instance state store

**Files:**
- Create: `internal/gateway/route_store.go`
- Create: `internal/gateway/instance_manager.go`
- Test: `internal/gateway/route_store_test.go`
- Test: `internal/gateway/instance_manager_test.go`

**Step 1: Write the failing tests**

Create `internal/gateway/route_store_test.go` for:

```go
func TestRegisterConnWritesUserRouteAndInstanceIndex(t *testing.T) {}
func TestGetUsersConnRefsFiltersByAliveAndRouteable(t *testing.T) {}
func TestGetUsersPresenceConnRefsFiltersOnlyByAlive(t *testing.T) {}
func TestUnregisterConnRemovesRouteAndIndex(t *testing.T) {}
```

Create `internal/gateway/instance_manager_test.go` for:

```go
func TestInstanceManagerWritesReadyRouteableAndDraining(t *testing.T) {}
func TestInstanceManagerReadyAndRouteableCanDiverge(t *testing.T) {}
```

Use Redis integration if the repo already has a Redis test helper; otherwise isolate encoding/parsing/unit behavior with fake Redis interfaces first.

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/gateway
```

Expected:
- FAIL because `RouteStore` and `InstanceManager` do not exist.

**Step 3: Write minimal implementation**

Create `internal/gateway/route_store.go` with:

```go
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
```

Implement minimal methods:
- `RegisterConn(ctx, conn RouteConn) error`
- `UnregisterConn(ctx, conn RouteConn) error`
- `GetUsersConnRefs(ctx, userIDs []string) (map[string][]RouteConn, error)`
- `GetUsersPresenceConnRefs(ctx, userIDs []string) (map[string][]RouteConn, error)`
- `ReconcileInstanceRoutes(ctx, instanceID string, conns []RouteConn) error` (stubbed minimally if not fully used yet, but real enough for tests)

Create `internal/gateway/instance_manager.go` with:

```go
type InstanceState struct {
    InstanceID string
    Ready      bool
    Routeable  bool
    Draining   bool
}
```

Implement:
- `Start(ctx)` heartbeat loop later; for now expose write/update helpers used by tests
- serialization to `nexo:instance:alive:{instance_id}`
- read helpers for route/presence filters

Do not add broadcast-related fields.
Do not overstate durability: route registration should be modeled as high-reliability plus eventual reconciliation, not a synchronous hard guarantee.

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/gateway
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/gateway/route_store.go internal/gateway/route_store_test.go internal/gateway/instance_manager.go internal/gateway/instance_manager_test.go
git commit -m "feat: add route store and instance state primitives"
```

---

### Task 6: Add route-only push bus and push coordinator

**Files:**
- Create: `internal/gateway/push_protocol.go`
- Create: `internal/gateway/push_bus.go`
- Create: `internal/gateway/push_coordinator.go`
- Modify: `internal/gateway/ws_server.go:24-252`
- Test: `internal/gateway/push_bus_test.go`
- Test: `internal/gateway/push_coordinator_test.go`

**Step 1: Write the failing tests**

Create `internal/gateway/push_coordinator_test.go`:

```go
func TestProcessPushTaskPushesLocalBeforePublishingRoute(t *testing.T) {}
func TestDispatchRouteOnlyPublishesPerInstanceEnvelope(t *testing.T) {}
func TestRouteReadFailureFallsBackToLocalOnly(t *testing.T) {}
func TestRemoteEnvelopeDeliversWithoutRedisLookup(t *testing.T) {}
```

Create `internal/gateway/push_bus_test.go`:

```go
func TestPublishToInstanceUsesInstanceChannel(t *testing.T) {}
func TestSubscribeInstanceInvokesHandler(t *testing.T) {}
func TestClosePreventsFurtherPublish(t *testing.T) {}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/gateway
```

Expected:
- FAIL because new coordinator, protocol, and bus types do not exist.

**Step 3: Write minimal implementation**

Create `internal/gateway/push_protocol.go` with the route-only envelope types:

```go
type PushMode string
const PushModeRoute PushMode = "route"

type PushContent struct { /* fields from design doc */ }
type PushPayload struct { /* fields from design doc */ }
type ConnRef struct { UserId string; ConnId string; PlatformId int }
type PushEnvelope struct {
    PushId         string
    Mode           PushMode
    TargetConnMap  map[string][]ConnRef
    SourceInstance string
    SentAt         int64
    Payload        *PushPayload
}
```

Create `internal/gateway/push_bus.go` with route-only methods:

```go
type PushBus interface {
    PublishToInstance(ctx context.Context, instanceID string, env *PushEnvelope) error
    SubscribeInstance(ctx context.Context, instanceID string, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) error
    Close() error
}
```

Create `internal/gateway/push_coordinator.go`:
- move `PushTask`
- move local push orchestration out of `WsServer`
- keep route-only logic only
- keep `AsyncPushToUsers(...)` matching `MessagePusher`
- return immediately if `sendClosed` / publish disabled / routeStore nil / bus nil

Update `WsServer` to:
- stop owning cross-instance publish logic directly
- expose helper(s) used by coordinator for local delivery
- keep `MessagePusher` compatibility either by:
  1. making coordinator implement `AsyncPushToUsers` and registering it with `MessageService`, or
  2. letting `WsServer.AsyncPushToUsers` delegate to coordinator

Recommended: keep `WsServer` implementing `MessagePusher` initially and delegate to `PushCoordinator` to minimize invasive changes.

Also ensure `excludeConnId` is filtered before both local delivery and remote per-instance grouping so single-connection exclusion semantics stay correct in multi-instance mode.

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/gateway
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/gateway/push_protocol.go internal/gateway/push_bus.go internal/gateway/push_bus_test.go internal/gateway/push_coordinator.go internal/gateway/push_coordinator_test.go internal/gateway/ws_server.go
git commit -m "feat: add route-only push coordinator and bus"
```

---

### Task 7: Remove Redis online writes from UserMap and add local route snapshots

**Files:**
- Modify: `internal/gateway/user_map.go:13-206`
- Test: `internal/gateway/user_map_test.go`

**Step 1: Write the failing tests**

Create `internal/gateway/user_map_test.go`:

```go
func TestRegisterDoesNotRequireRedisSideEffect(t *testing.T) {
    m := NewUserMap(nil)
    client := &Client{UserId: "u1", ConnId: "c1", PlatformId: 1}
    m.Register(context.Background(), client)
    clients, ok := m.GetAll("u1")
    require.True(t, ok)
    require.Len(t, clients, 1)
}

func TestSnapshotRouteConnsReturnsAllLocalConnections(t *testing.T) {
    m := NewUserMap(nil)
    m.Register(context.Background(), &Client{UserId: "u1", ConnId: "c1", PlatformId: 1})
    m.Register(context.Background(), &Client{UserId: "u1", ConnId: "c2", PlatformId: 2})
    got := m.SnapshotRouteConns("inst-a")
    require.ElementsMatch(t, []RouteConn{
        {UserId: "u1", ConnId: "c1", PlatformId: 1, InstanceId: "inst-a"},
        {UserId: "u1", ConnId: "c2", PlatformId: 2, InstanceId: "inst-a"},
    }, got)
}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/gateway
```

Expected:
- FAIL because `SnapshotRouteConns` does not exist and tests cannot assert the new boundary.

**Step 3: Write minimal implementation**

In `internal/gateway/user_map.go`:
- keep local connection map behavior unchanged
- remove `setOnline(...)` from `Register`
- remove `setOffline(...)` from `Unregister`
- leave `IsOnline(...)` temporarily intact for compatibility, but keep it out of correctness decisions
- add:

```go
func (m *UserMap) SnapshotRouteConns(instanceId string) []RouteConn {
    m.mu.RLock()
    defer m.mu.RUnlock()

    conns := make([]RouteConn, 0)
    for userId, up := range m.users {
        for _, client := range up.Clients {
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
```

This task intentionally happens after `PresenceService` is in place so removing Redis online side effects does not create a migration gap.

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/gateway
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/gateway/user_map.go internal/gateway/user_map_test.go
git commit -m "refactor: make user map local-only and expose route snapshots"
```

---

### Task 8: Add cross-instance config, startup wiring, and graceful shutdown orchestration

**Files:**
- Modify: `internal/config/config.go:10-161`
- Modify: `cmd/server/main.go:22-107`
- Modify: `internal/router/router.go:17-77`
- Test: `internal/config/config_test.go`
- Test: `cmd/server/main_test.go`

**Step 1: Write the failing tests**

Create `internal/config/config_test.go`:

```go
func TestLoadSetsCrossInstanceDefaults(t *testing.T) {
    cfg := loadTestConfig(t, "testdata/minimal.yaml")
    require.False(t, cfg.WebSocket.CrossInstance.Enabled)
    require.Equal(t, 10, cfg.WebSocket.CrossInstance.HeartbeatSecond)
    require.Equal(t, 70, cfg.WebSocket.CrossInstance.RouteTTLSeconds)
}
```

Create `cmd/server/main_test.go` for extracted wiring helpers, not `main()` directly:

```go
func TestBuildServerDependenciesGeneratesInstanceIDWhenEmpty(t *testing.T) {}
func TestBuildServerDependenciesWiresMessageHandlerWithGate(t *testing.T) {}
func TestBuildServerDependenciesWiresUserHandlerWithPresenceService(t *testing.T) {}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/config ./cmd/server
```

Expected:
- FAIL because cross-instance config and extracted wiring helpers do not exist.

**Step 3: Write minimal implementation**

In `internal/config/config.go`, add:

```go
type CrossInstanceConfig struct {
    Enabled                    bool   `mapstructure:"enabled"`
    InstanceID                 string `mapstructure:"instance_id"`
    HeartbeatSecond            int    `mapstructure:"heartbeat_second"`
    RouteTTLSeconds            int    `mapstructure:"route_ttl_seconds"`
    InstanceAliveTTLSeconds    int    `mapstructure:"instance_alive_ttl_seconds"`
    PublishFailThreshold       int    `mapstructure:"publish_fail_threshold"`
    RouteSubscribeFailThreshold int   `mapstructure:"route_subscribe_fail_threshold"`
    PresenceReadFailThreshold  int    `mapstructure:"presence_read_fail_threshold"`
    RecoverProbeIntervalSeconds int   `mapstructure:"recover_probe_interval_seconds"`
    DrainTimeoutSeconds        int    `mapstructure:"drain_timeout_seconds"`
    DrainRouteableGraceSeconds int    `mapstructure:"drain_routeable_grace_seconds"`
    DrainKickBatchSize         int    `mapstructure:"drain_kick_batch_size"`
    DrainKickIntervalMs        int    `mapstructure:"drain_kick_interval_ms"`
}
```

and embed under `WebSocketConfig`.

Set defaults matching the design doc.

In `cmd/server/main.go`:
- extract wiring into helper(s) so tests can validate composition
- create/calculate `instanceID` if empty (e.g. hostname-port-uuid suffix)
- keep `WsServer` as the `MessagePusher` registered into `MessageService`, and let `PushCoordinator` attach as an internal collaborator rather than replacing the public pusher identity in phase 1
- instantiate:
  - `LifecycleGate`
  - `RouteStore`
  - `InstanceManager`
  - `PushBus`
  - `PushCoordinator`
  - `PresenceService`
- when `cross_instance.enabled=false`, allow `RouteStore / InstanceManager / PushBus` to stay nil, disabled, or no-op; multi-instance component init failure must not break the single-instance startup path
- pass gate into `NewMessageHandler(...)`
- pass presence service into `NewUserHandler(...)`
- pass readiness/gate into `SetupRouter(...)`
- on signal:
  1. mark gate unready/close ingress/begin send drain
  2. stop Hertz ingress
  3. wait for send drain (extract helper if needed)
  4. close send path
  5. stop instance manager / push bus

Keep first implementation simple and explicit; do not over-abstract startup wiring.

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/config ./cmd/server
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go cmd/server/main.go cmd/server/main_test.go internal/router/router.go
git commit -m "feat: wire route-only multi-instance startup and shutdown"
```

---

### Task 9: Add reconcile/repair and shutdown regression tests

**Files:**
- Modify: `internal/gateway/route_store.go`
- Modify: `internal/gateway/ws_server.go`
- Modify: `internal/gateway/push_coordinator.go`
- Test: `internal/gateway/route_reconcile_test.go`
- Test: `internal/gateway/shutdown_test.go`

**Step 1: Write the failing tests**

Create `internal/gateway/route_reconcile_test.go`:

```go
func TestReconcileRepairsMissingRouteEntry(t *testing.T) {}
func TestReconcileRemovesStaleInstanceOwnedConn(t *testing.T) {}
```

Create `internal/gateway/shutdown_test.go`:

```go
func TestShutdownRejectsNewSendsButAllowsInflightLeaseToFinish(t *testing.T) {}
func TestPlannedDrainKeepsRouteableTrueUntilConnectionsDrain(t *testing.T) {}
func TestSubscribeFaultMarksInstanceUnrouteableImmediately(t *testing.T) {}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/gateway
```

Expected:
- FAIL because reconcile and shutdown orchestration are not complete.

**Step 3: Write minimal implementation**

Implement the smallest missing pieces to satisfy lifecycle correctness:
- route repair worker / helper for pending register recovery
- `ReconcileInstanceRoutes(...)` authoritative diff using instance-owned set
- shutdown helper(s) that preserve:
  - planned/manual: `ready=0`, later `routeable=0`
  - subscribe fault: immediate `routeable=0`
- ensure `UnregisterClient(...)` behavior during shutdown does not silently lose required drain convergence events

If `unregisterChan` is still lossy, change it now so shutdown paths can block briefly or use a bounded fallback store instead of silent drop.

When testing subscribe faults, only instance-level subscription failure should trigger drain. Single envelope decode errors or one-off publish failures should be asserted as non-fatal.

**Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/gateway
```

Expected:
- PASS

**Step 5: Commit**

```bash
git add internal/gateway/route_store.go internal/gateway/push_coordinator.go internal/gateway/ws_server.go internal/gateway/route_reconcile_test.go internal/gateway/shutdown_test.go
git commit -m "feat: add route repair and shutdown correctness checks"
```

---

### Task 10: Run full targeted verification for phase 1

**Files:**
- Modify: none
- Test: existing and newly added tests above

**Step 1: Run gateway tests**

Run:

```bash
go test ./internal/gateway -count=1
```

Expected:
- PASS

**Step 2: Run handler, service, config, router, response tests**

Run:

```bash
go test ./internal/handler ./internal/service ./internal/config ./internal/router ./pkg/response -count=1
```

Expected:
- PASS

**Step 3: Run project-wide verification for touched packages**

Run:

```bash
go test ./... -count=1
```

Expected:
- PASS

**Step 4: Review final diff before integration**

Run:

```bash
git diff --stat
git diff -- internal/gateway internal/handler internal/service internal/config internal/router cmd/server pkg/response pkg/errcode
```

Expected:
- Only the intended phase-1 files changed.

**Step 5: Commit final verification checkpoint**

```bash
git add internal/gateway internal/handler internal/service internal/config internal/router cmd/server pkg/response pkg/errcode
git commit -m "test: verify route-only multi-instance push phase 1"
```

---

## Notes for the implementer

- Use @superpowers:test-driven-development or @everything-claude-code:go-test before implementation.
- Use @superpowers:executing-plans when executing this file in another session.
- Do not introduce broadcast code paths in phase 1.
- Do not let `UserHandler` or `MessageHandler` depend directly on `WsServer` internals once the new services/interfaces exist.
- Keep public behavior unchanged when `cross_instance.enabled=false`.
- Preserve the current online-status response shape while changing its data source.
- Keep online-status DTOs out of `gateway/ws_server.go` so service-layer code does not depend on gateway internals.
- Prefer explicit wiring in `cmd/server/main.go` over premature dependency injection frameworks.
