# 多实例混合推送最终方案

**文档版本:** 1.2
**创建日期:** 2026-03-03
**修订日期:** 2026-03-03
**状态:** 已批准

### 修订记录

| 版本 | 日期 | 变更摘要 |
|------|------|---------|
| 1.0 | 2026-03-03 | 初始方案 |
| 1.1 | 2026-03-03 | 修复 dispatchCrossInstance 用户级差集导致同用户多实例连接丢推送；PushEnvelope 传输结构从 entity.Message 改为轻量 MessageData；RouteStore 调用从 UserMap 移至 WsServer 层保持职责分离；补充路由 TTL 心跳续期机制；Route 模式改为按实例拆分独立信封减少带宽；去重清理从 per-message goroutine 改为定期批量清理；修正字段引用错误和返回类型兼容性 |
| 1.2 | 2026-03-03 | WsServer 新增内部 ctx/cancel 解决 goroutine 生命周期管理；unregisterClient 路由注销改为带超时异步避免阻塞 eventLoop；registerClient 路由注册 goroutine 加超时 context；GetUsersOnlineStatus 合并本地+远端连接结果；messageToMsgData 提升至 processPushTask 统一转换一次；新增类型命名统一为现有 Id 风格；groupByInstance 去掉冗余 instanceID 参数；补充 collectInstances/countTargets/groupByInstance 辅助函数定义；补充配置默认值设置；补充在线状态双源说明和 PushEnvelope 大小限制说明；修正组件交互图 PushBus 描述 |

---

## 目录

1. [概述](#概述)
2. [设计决策](#设计决策)
3. [架构设计](#架构设计)
4. [数据结构](#数据结构)
5. [核心组件](#核心组件)
6. [关键流程](#关键流程)
7. [配置说明](#配置说明)
8. [错误处理与降级](#错误处理与降级)
9. [改动范围](#改动范围)
10. [测试策略](#测试策略)
11. [验收标准](#验收标准)

---

## 概述

### 问题背景

当前 nexo_v2 采用单实例 WebSocket 架构，`processPushTask` 只能推送给本实例 `UserMap` 中的连接。多实例部署时跨实例用户无法实时推送。

### 解决方案

基于 Redis Pub/Sub 的混合路由策略，在 gateway 层增加跨实例推送能力：

- **单聊消息**：精准路由（查路由表，只发给目标实例）
- **小群消息**（在线目标数 ≤ 阈值）：精准路由
- **大群消息**（在线目标数 > 阈值）：全局广播

保持 `MessagePusher` 接口和 `MessageService` 完全不变，改动集中在 gateway 层。通过双开关（`cross_instance.enabled` + `hybrid.enabled`）支持分步灰度和一键回退。

### 设计目标

- 支持 5-20 个实例规模
- 消息推送延迟 < 100ms（单聊）、< 200ms（大群广播）
- 高可靠性：Redis 不可用时降级为本地推送，消息不丢失（DB + pull 兜底）
- 向后兼容：配置关闭后行为与改造前完全一致
- 可灰度：支持先开跨实例基础设施 + 强制广播，再逐步切换到混合策略

---

## 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 路由粒度 | **连接级**（ConnId + InstanceId + PlatformId） | 现有 Client 已有 ConnId/PlatformId，支持同一用户多端在不同实例时精确排除发送者连接 |
| 组件拆分 | **四组件独立**（InstanceManager / RouteStore / PushBus / HybridSelector） | 职责清晰，单独可测，不污染现有 UserMap 核心逻辑 |
| 策略阈值 | **多阈值 + ForceBroadcast 开关** | RouteMaxTargets + RouteMaxInstances + ForceBroadcastMinTargets 三级判断，外加 ForceBroadcast 灰度开关 |
| 推送协议 | **PushEnvelope 信封 + TargetConnMap** | route 模式在信封中按实例预分组连接信息，接收端零查询直接投递 |
| 命名风格 | **统一使用现有 `Id` 后缀** | 现有 Client/PlatformStatusDetail 均使用 `UserId`/`ConnId`/`PlatformId`，新增类型保持一致，避免同包内两种命名风格混用 |
| 消息去重 | **本地 sync.Map + 定期清理** | 广播模式下实例级去重，避免重复推送，单 goroutine 每秒批量清理过期条目 |
| 接口抽象 | **PushBus 接口独立文件** | 方便单元测试 mock，未来可替换为 Redis Stream 等实现 |
| 开关设计 | **双开关** | `cross_instance.enabled` 控制基础设施（路由表、心跳），`hybrid.enabled` 控制策略选择；可分步灰度 |
| WsServer 改造 | **拆分三方法** | `pushToLocalClients` + `dispatchCrossInstance` + `onRemoteEnvelope`，可测试性好 |

---

## 架构设计

### 系统架构图

```
MessageService.SendMessage()
    ↓ (接口不变: MessagePusher.AsyncPushToUsers)
WsServer.AsyncPushToUsers(msg, userIds, excludeConnId)
    ↓
pushChan → pushLoop → processPushTask()  [改造点]
    │
    ├─ msgData := messageToMsgData(task.Msg)  [统一转换一次]
    │
    ├─ pushToLocalClients(task, msgData)
    │   UserMap.GetAll(userId) → client.PushMessage(msgData)
    │   返回 localDelivered: map[userId][]ConnRef
    │
    ├─ cross_instance.enabled == false? → 结束
    │
    └─ dispatchCrossInstance(task, localDelivered, msgData)
        │
        ├─ RouteStore.GetUsersConnRefs(task.TargetIds)  [查询所有目标用户]
        │   → map[userId][]ConnRefOnInstance
        │
        ├─ filterOutLocalInstance(routeMap, selfInstanceId)
        │   → remoteRouteMap (排除本实例已投递的连接)
        │
        ├─ HybridSelector.Select(sessionType, remoteTargetCount, instanceCount)
        │   → PushMode (route | broadcast)
        │
        ├─ Route 模式:
        │   groupByInstance(remoteRouteMap) → 按实例分组
        │   按实例拆分独立信封 (每个信封只含目标实例的 ConnRef)
        │   → PushBus.PublishToInstance(instanceId, perInstanceEnvelope)
        │
        └─ Broadcast 模式:
            → PushBus.PublishBroadcast(envelope)
```

### 组件交互图

```
┌──────────────────────────────────────────────────────────────┐
│                         WsServer                              │
│  ┌────────────┐  ┌──────────────────┐  ┌──────────────────┐ │
│  │  UserMap    │  │ pushLoop workers │  │  eventLoop       │ │
│  │ (本地连接) │  │ (推送分发)       │  │ (register/unreg) │ │
│  └─────┬──────┘  └────────┬─────────┘  └────────┬─────────┘ │
│        │                  │                      │            │
│  ┌─────┴──────────────────┴──────────────────────┴──────────┐│
│  │         跨实例组件 (cross_instance.enabled=true)          ││
│  │                                                           ││
│  │  ┌─────────────────┐  ┌───────────────────────────────┐  ││
│  │  │ InstanceManager │  │ PushBus (接口)                │  ││
│  │  │ - 实例注册/心跳 │  │ - PublishToInstance            │  ││
│  │  │ - 优雅关闭      │  │ - PublishBroadcast             │  ││
│  │  └─────────────────┘  │ - SubscribeInstance            │  ││
│  │                        │ - SubscribeBroadcast           │  ││
│  │  ┌─────────────────┐  │ - Close                        │  ││
│  │  │ RouteStore      │  └───────────────────────────────┘  ││
│  │  │ - 连接级路由表  │                                      ││
│  │  │ - RegisterConn  │  ┌───────────────────────────────┐  ││
│  │  │ - GetUserConns  │  │ HybridSelector                │  ││
│  │  └─────────────────┘  │ - 多阈值 + ForceBroadcast     │  ││
│  │                        └───────────────────────────────┘  ││
│  └───────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────┘
                          ↕ Redis Pub/Sub
              ┌───────────┴───────────┐
              ↓                       ↓
         其他实例 1              其他实例 2
```

---

## 数据结构

### 1. 实例注册信息

**Key:** `nexo:instance:alive:{instance_id}`
**类型:** Hash
**TTL:** 30 秒（心跳每 10 秒刷新）

```json
{
    "instance_id": "server1-8080-a1b2c3",
    "host": "server1",
    "port": "8080",
    "started_at": "1709366400000",
    "conn_count": "1234",
    "last_heartbeat": "1709366430000"
}
```

### 2. 连接级路由表

**Key:** `nexo:route:user:{user_id}`
**类型:** Hash（field = connId, value = `instance_id|conn_id|platform_id`）
**TTL:** 70 秒（比心跳长，避免竞态；由心跳回调定期续期）

```
HSET nexo:route:user:user123 conn-uuid-1 "server1-8080|conn-uuid-1|1"
HSET nexo:route:user:user123 conn-uuid-2 "server2-8081|conn-uuid-2|5"
```

**为什么用 Hash + 管道分隔？**
- 一个用户可能有多个连接（手机 + 电脑），每个连接是一个 field
- 支持单独注册/注销某个连接，不影响其他连接
- 管道分隔比 JSON 更轻量（省序列化开销），字段固定无需扩展性

### 3. Redis Channel 设计

**精准路由 Channel:**
- 格式: `nexo:push:instance:{instance_id}`
- 每个实例订阅自己的 channel

**全局广播 Channel:**
- 格式: `nexo:push:broadcast`
- 所有实例都订阅

### 4. 推送信封 (PushEnvelope)

```go
type PushMode string
const (
    PushModeRoute     PushMode = "route"
    PushModeBroadcast PushMode = "broadcast"
)

type ConnRef struct {
    UserId     string `json:"user_id"`
    ConnId     string `json:"conn_id"`
    PlatformId int    `json:"platform_id"`
}

type PushEnvelope struct {
    PushId         string                `json:"push_id"`                    // UUID，去重用
    MsgId          int64                 `json:"msg_id"`                     // 消息 ID（对应 entity.Message.Id）
    ConversationId string                `json:"conversation_id"`            // 会话 ID
    SessionType    int32                 `json:"session_type"`               // 会话类型
    Mode           PushMode              `json:"mode"`                       // route | broadcast
    TargetUserIds  []string              `json:"target_user_ids,omitempty"`  // broadcast 模式: 目标用户
    TargetConnMap  map[string][]ConnRef  `json:"target_conn_map,omitempty"`  // route 模式: 仅含目标实例的连接
    ExcludeConnId  string                `json:"exclude_conn_id,omitempty"`  // 排除的连接 ID
    MsgData        *MessageData          `json:"msg_data"`                   // 轻量消息体（复用 protocol.go 的 MessageData）
    SourceInstance string                `json:"source_instance"`            // 来源实例 ID
    SentAt         int64                 `json:"sent_at"`                    // 时间戳
}
```

**route 模式**使用 `TargetConnMap`：发送端按实例拆分独立信封，每个信封只包含目标实例的 ConnRef，接收端零查询直接投递。
**broadcast 模式**使用 `TargetUserIds`：接收端按本地 UserMap 过滤。
**MsgData 字段**使用 `MessageData`（已在 `protocol.go` 中定义的轻量结构），避免传输 `entity.Message` 中的 GORM 元字段（CreatedAt、UpdatedAt 等）和冗余存储字段。

**PushEnvelope 大小限制说明（v1.2 新增）：**
`MsgData` 包含完整消息内容（Content 字段）。对于大文件消息（如 ContentImage 含 base64 数据），序列化后 envelope 可能较大。Redis Pub/Sub 消息会占用订阅者的 output buffer（默认 `client-output-buffer-limit pubsub 32mb 8mb 60`）。
- **当前阶段**：nexo_v2 的 `MaxMessageSize` 默认 50KB（`config.go:138`），单条 envelope 序列化后不超过 100KB，在 Redis 默认限制内安全运行
- **后续优化方向**：如果未来支持更大消息体（如视频预览），可考虑 envelope 只传 `MsgId`，接收端通过 DB 获取完整消息（trade-off 是增加延迟和 DB 压力）

---

## 核心组件

### 1. InstanceManager（实例管理器）

**文件:** `internal/gateway/instance_manager.go`

**职责:** 管理本实例的生命周期——ID 生成、注册、心跳、关闭。独立组件，不与 PushBus 耦合。

```go
type InstanceManager struct {
    instanceId     string
    rdb            *redis.Client
    aliveTTL       time.Duration
    heartbeat      time.Duration
    stopChan       chan struct{}
    onHeartbeat    func(ctx context.Context)  // 心跳回调，用于路由 TTL 续期等
}

func NewInstanceManager(instanceId string, rdb *redis.Client, aliveTTL, heartbeat time.Duration) *InstanceManager
func (m *InstanceManager) InstanceId() string
func (m *InstanceManager) SetOnHeartbeat(fn func(ctx context.Context))  // 注册心跳回调
func (m *InstanceManager) Start(ctx context.Context)         // 注册 + 启动心跳循环
func (m *InstanceManager) Stop(ctx context.Context)          // 停止心跳，清理注册
func (m *InstanceManager) register(ctx context.Context) error
func (m *InstanceManager) heartbeatLoop(ctx context.Context) // 每次心跳后调用 onHeartbeat
```

**实例 ID 生成规则（resolveInstanceId）:**
1. 配置 `websocket.cross_instance.instance_id` 非空 → 直接使用
2. 环境变量 `NEXO_INSTANCE_ID` 非空 → 使用
3. 都为空 → `hostname-port-随机后缀(6位)`

### 2. RouteStore（路由存储）

**文件:** `internal/gateway/route_store.go`

**职责:** 维护连接级路由表（Redis Hash），支持注册、注销、查询。

```go
type ConnRefOnInstance struct {
    UserId     string
    ConnId     string
    InstanceId string
    PlatformId int
}

type RouteStore struct {
    rdb        *redis.Client
    instanceId string
    routeTTL   time.Duration
}

func NewRouteStore(rdb *redis.Client, instanceId string, routeTTL time.Duration) *RouteStore
func (s *RouteStore) RegisterConn(ctx context.Context, client *Client) error
func (s *RouteStore) UnregisterConn(ctx context.Context, client *Client) error
func (s *RouteStore) RefreshRouteTTL(ctx context.Context, userId string) error
func (s *RouteStore) RefreshAllConnRoutes(ctx context.Context, userIds []string) error  // 批量续期
func (s *RouteStore) GetUserConnRefs(ctx context.Context, userId string) ([]ConnRefOnInstance, error)
func (s *RouteStore) GetUsersConnRefs(ctx context.Context, userIds []string) (map[string][]ConnRefOnInstance, error)
```

**存储格式:** `HSET nexo:route:user:{userId} {connId} "{instanceId}|{connId}|{platformId}"`
**批量查询:** `GetUsersConnRefs` 使用 Redis Pipeline，一次往返查询所有用户路由。
**批量续期:** `RefreshAllConnRoutes` 使用 Redis Pipeline 批量 EXPIRE，在心跳回调中调用。
**锁外执行:** 所有 Redis 操作在 WsServer 层（UserMap 锁外）异步执行，不阻塞本地连接管理。

### 3. PushBus（推送总线接口）

**文件:** `internal/gateway/push_bus.go`

**职责:** 抽象 Redis Pub/Sub 的发布与订阅，方便测试 mock 和未来替换实现。

```go
type PushBus interface {
    PublishToInstance(ctx context.Context, instanceId string, env *PushEnvelope) error
    PublishBroadcast(ctx context.Context, env *PushEnvelope) error
    SubscribeInstance(ctx context.Context, instanceId string, handler func(context.Context, *PushEnvelope)) error
    SubscribeBroadcast(ctx context.Context, handler func(context.Context, *PushEnvelope)) error
    Close() error
}

type RedisPushBus struct {
    rdb    *redis.Client
    pubsub *redis.PubSub
}

func NewRedisPushBus(rdb *redis.Client) *RedisPushBus
```

**序列化:** `json.Marshal`/`json.Unmarshal`，反序列化失败写日志并丢弃单条消息。
**重连:** 依赖 go-redis/v9 PubSub 内置的自动重连机制（`ReceiveMessage` 内部处理断线重连），不自行实现重连逻辑以避免冲突。当 `ReceiveMessage` 返回持久性错误（非重试错误）时，降级为本地推送模式并记录告警日志。

### 4. HybridSelector（策略选择器）

**文件:** `internal/gateway/push_selector.go`

**职责:** 根据消息类型和在线目标数决定使用精准路由还是广播。

```go
type HybridPolicy struct {
    Enabled                       bool
    GroupRouteMaxTargets          int   // 默认 100
    GroupRouteMaxInstances        int   // 默认 4
    ForceBroadcastGroupMinTargets int   // 默认 500
    ForceBroadcast                bool  // true 时群聊全走广播（灰度用）
}

type HybridSelector struct {
    policy HybridPolicy
}

func NewHybridSelector(policy HybridPolicy) *HybridSelector
func (s *HybridSelector) Select(sessionType int32, onlineTargets int, instanceCount int) PushMode
```

**决策规则:**

```
if !Enabled:
    return Broadcast  // hybrid 关闭时群聊全广播

if 单聊 (sessionType == SingleChat):
    return Route

if ForceBroadcast:
    return Broadcast  // 灰度阶段强制广播

if onlineTargets >= ForceBroadcastGroupMinTargets:
    return Broadcast

if onlineTargets <= GroupRouteMaxTargets && instanceCount <= GroupRouteMaxInstances:
    return Route

return Broadcast
```

---

## 关键流程

### 1. processPushTask 改造（核心改动）

将现有 `processPushTask` 拆分为三个可测试方法：

> **v1.2 修正:** `messageToMsgData` 在 `processPushTask` 中统一转换一次，作为参数传入子方法，
> 避免 `pushToLocalClients` 和 `dispatchCrossInstance` 各自重复转换。

```go
func (s *WsServer) processPushTask(ctx context.Context, task *PushTask) {
    // 统一转换一次，避免子方法重复调用
    msgData := s.messageToMsgData(task.Msg)

    // Step 1: 本地推送
    localDelivered := s.pushToLocalClients(ctx, task, msgData)

    // Step 2: 跨实例推送
    if s.cfg.WebSocket.CrossInstance.Enabled && s.pushBus != nil {
        s.dispatchCrossInstance(ctx, task, localDelivered, msgData)
    }
}
```

### 2. pushToLocalClients — 本地推送（保持现有逻辑）

```go
func (s *WsServer) pushToLocalClients(ctx context.Context, task *PushTask, msgData *MessageData) map[string][]ConnRef {
    localDelivered := make(map[string][]ConnRef)

    for _, userId := range task.TargetIds {
        clients, ok := s.userMap.GetAll(userId)
        if !ok {
            continue
        }
        for _, client := range clients {
            if task.ExcludeId != "" && client.ConnId == task.ExcludeId {
                continue
            }
            if err := client.PushMessage(ctx, msgData); err == nil {
                localDelivered[userId] = append(localDelivered[userId], ConnRef{
                    UserId: userId, ConnId: client.ConnId, PlatformId: client.PlatformId,
                })
            }
        }
    }
    return localDelivered
}
```

### 3. dispatchCrossInstance — 跨实例分发

> **v1.1 关键修正:** 过滤粒度从用户级改为连接级。查询所有目标用户的路由，
> 排除本实例连接后再分发。解决同一用户多端在不同实例时远端连接丢推送的问题。

> **v1.2 修正:** `msgData` 由 `processPushTask` 预转换后传入，不再重复调用 `messageToMsgData`。
> `groupByInstance` 去掉冗余的 `instanceId` 参数（`remoteRouteMap` 已排除本实例连接）。
> 补充 `collectInstances`、`countTargets`、`groupByInstance` 辅助函数定义。

```go
func (s *WsServer) dispatchCrossInstance(ctx context.Context, task *PushTask, localDelivered map[string][]ConnRef, msgData *MessageData) {
    // 1. 查询所有目标用户的路由（不做用户级过滤，避免丢失同用户跨实例连接）
    routeMap, err := s.routeStore.GetUsersConnRefs(ctx, task.TargetIds)
    if err != nil {
        // 路由查询失败 → 回退广播（宁可多发不漏发）
        s.fallbackBroadcast(ctx, task, msgData)
        return
    }

    // 2. 连接级过滤：移除本实例的连接（已由 pushToLocalClients 处理）
    remoteRouteMap := filterOutLocalInstance(routeMap, s.instanceId)
    if len(remoteRouteMap) == 0 {
        return
    }

    // 3. 统计远端目标数和实例数
    instanceSet := collectInstances(remoteRouteMap)
    remoteTargets := countTargets(remoteRouteMap)

    // 4. 选择策略
    mode := s.selector.Select(task.Msg.SessionType, remoteTargets, len(instanceSet))

    // 5. 构建信封基础字段（msgData 由调用方预转换，不再重复调用 messageToMsgData）
    baseEnv := PushEnvelope{
        PushId:         uuid.New().String(),
        MsgId:          task.Msg.Id,
        ConversationId: task.Msg.ConversationId,
        SessionType:    task.Msg.SessionType,
        ExcludeConnId:  task.ExcludeId,
        MsgData:        msgData,
        SourceInstance: s.instanceId,
        SentAt:         time.Now().UnixMilli(),
    }

    switch mode {
    case PushModeRoute:
        // 按实例拆分独立信封，每个信封只含目标实例的 ConnRef（节省带宽）
        grouped := groupByInstance(remoteRouteMap)
        for instId, connRefs := range grouped {
            instEnv := baseEnv
            instEnv.Mode = PushModeRoute
            instEnv.TargetConnMap = map[string][]ConnRef{instId: connRefs}
            if err := s.pushBus.PublishToInstance(ctx, instId, &instEnv); err != nil {
                log.CtxWarn(ctx, "publish to instance %s failed: %v", instId, err)
            }
        }
    case PushModeBroadcast:
        broadEnv := baseEnv
        broadEnv.Mode = PushModeBroadcast
        broadEnv.TargetUserIds = task.TargetIds  // 所有目标用户，各实例按本地 UserMap 过滤
        if err := s.pushBus.PublishBroadcast(ctx, &broadEnv); err != nil {
            log.CtxWarn(ctx, "publish broadcast failed: %v", err)
        }
    }
}

// filterOutLocalInstance 移除属于本实例的连接，返回仅包含远端连接的路由表
func filterOutLocalInstance(routeMap map[string][]ConnRefOnInstance, selfId string) map[string][]ConnRefOnInstance {
    result := make(map[string][]ConnRefOnInstance)
    for userId, refs := range routeMap {
        for _, ref := range refs {
            if ref.InstanceId != selfId {
                result[userId] = append(result[userId], ref)
            }
        }
    }
    return result
}

// collectInstances 收集远端路由表中涉及的所有实例 ID（去重）
func collectInstances(routeMap map[string][]ConnRefOnInstance) map[string]bool {
    instances := make(map[string]bool)
    for _, refs := range routeMap {
        for _, ref := range refs {
            instances[ref.InstanceId] = true
        }
    }
    return instances
}

// countTargets 统计远端路由表中的总连接数
func countTargets(routeMap map[string][]ConnRefOnInstance) int {
    count := 0
    for _, refs := range routeMap {
        count += len(refs)
    }
    return count
}

// groupByInstance 将远端路由表按实例 ID 分组，返回 map[instanceId][]ConnRef
// 注意：传入的 routeMap 应已排除本实例连接（通过 filterOutLocalInstance），因此无需再过滤
func groupByInstance(routeMap map[string][]ConnRefOnInstance) map[string][]ConnRef {
    grouped := make(map[string][]ConnRef)
    for _, refs := range routeMap {
        for _, ref := range refs {
            grouped[ref.InstanceId] = append(grouped[ref.InstanceId], ConnRef{
                UserId:     ref.UserId,
                ConnId:     ref.ConnId,
                PlatformId: ref.PlatformId,
            })
        }
    }
    return grouped
}

// fallbackBroadcast 路由查询失败时回退为广播模式
func (s *WsServer) fallbackBroadcast(ctx context.Context, task *PushTask, msgData *MessageData) {
    env := &PushEnvelope{
        PushId:         uuid.New().String(),
        MsgId:          task.Msg.Id,
        ConversationId: task.Msg.ConversationId,
        SessionType:    task.Msg.SessionType,
        Mode:           PushModeBroadcast,
        TargetUserIds:  task.TargetIds,
        ExcludeConnId:  task.ExcludeId,
        MsgData:        msgData,
        SourceInstance: s.instanceId,
        SentAt:         time.Now().UnixMilli(),
    }
    if err := s.pushBus.PublishBroadcast(ctx, env); err != nil {
        log.CtxWarn(ctx, "fallback broadcast failed: %v", err)
    }
}
```

### 4. onRemoteEnvelope — 接收端处理

```go
func (s *WsServer) onRemoteEnvelope(ctx context.Context, env *PushEnvelope) {
    // 1. 去重（本地 sync.Map，定期批量清理）
    if _, loaded := s.pushDedup.LoadOrStore(env.PushId, time.Now().UnixMilli()); loaded {
        return
    }

    // 2. 直接使用信封中的 MsgData（已是轻量 MessageData，无需再次转换）
    msgData := env.MsgData

    switch env.Mode {
    case PushModeRoute:
        // route 模式: 信封中只包含本实例的连接（发送端已按实例拆分）
        connRefs, ok := env.TargetConnMap[s.instanceId]
        if !ok {
            return
        }
        for _, ref := range connRefs {
            clients, ok := s.userMap.GetAll(ref.UserId)
            if !ok {
                continue
            }
            for _, client := range clients {
                if client.ConnId == ref.ConnId {
                    client.PushMessage(ctx, msgData)
                    break
                }
            }
        }

    case PushModeBroadcast:
        // broadcast 模式: 按 TargetUserIds 过滤本地在线用户
        for _, userId := range env.TargetUserIds {
            clients, ok := s.userMap.GetAll(userId)
            if !ok {
                continue
            }
            for _, client := range clients {
                if env.ExcludeConnId != "" && client.ConnId == env.ExcludeConnId {
                    continue
                }
                client.PushMessage(ctx, msgData)
            }
        }
    }
}

// startDedupCleaner 启动去重表定期清理（单 goroutine，每秒清理过期条目）
func (s *WsServer) startDedupCleaner(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Second)
    go func() {
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                now := time.Now().UnixMilli()
                s.pushDedup.Range(func(key, value any) bool {
                    if ts, ok := value.(int64); ok && now-ts > 5000 {
                        s.pushDedup.Delete(key)
                    }
                    return true
                })
            }
        }
    }()
}
```

### 5. 连接注册/注销时更新路由

> **v1.1 修正:** RouteStore 调用从 UserMap 移至 WsServer 的 registerClient/unregisterClient，
> 保持 UserMap 仅负责本地连接管理，不引入分布式路由依赖（符合"四组件独立"设计决策）。

> **v1.2 修正:** register 和 unregister 路由操作均改为异步 goroutine + 带超时 context，
> 避免 Redis 慢响应阻塞 eventLoop（eventLoop 是单 goroutine 串行处理 register/unregister，
> 任何同步 Redis I/O 都会造成级联阻塞）。

```go
// WsServer.registerClient 中新增路由注册（UserMap 不改动）
func (s *WsServer) registerClient(ctx context.Context, client *Client) {
    // ... 现有逻辑保持不变（UserMap.Register + 计数器更新） ...

    // 异步注册到分布式路由表（失败不影响本地注册）
    if s.routeStore != nil {
        go func() {
            rctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
            defer cancel()
            if err := s.routeStore.RegisterConn(rctx, client); err != nil {
                log.Warn("register route failed: user=%s conn=%s err=%v",
                    client.UserId, client.ConnId, err)
            }
        }()
    }
}

// WsServer.unregisterClient 中新增路由注销
func (s *WsServer) unregisterClient(ctx context.Context, client *Client) {
    // ... 现有逻辑保持不变（UserMap.Unregister + 计数器更新） ...

    // 异步注销路由（带超时，避免 Redis 慢响应阻塞 eventLoop）
    if s.routeStore != nil {
        go func() {
            rctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
            defer cancel()
            if err := s.routeStore.UnregisterConn(rctx, client); err != nil {
                log.Warn("unregister route failed: user=%s conn=%s err=%v",
                    client.UserId, client.ConnId, err)
            }
        }()
    }
}
```

**UserMap 不改动：** `Register` / `Unregister` 方法保持原样，不新增 `routeStore` 字段。

### 6. 在线状态查询多实例聚合

> **v1.2 修正:** 合并本地连接 + 远端连接（通过 RouteStore 查询、排除本实例后合并），
> 解决同一用户在本地和远端都有连接时只返回本地连接、远端连接丢失的问题。
> 保持现有返回类型 `[]*OnlineStatusResult`（含 DetailPlatformStatus），避免 breaking change。

```go
func (s *WsServer) GetUsersOnlineStatus(userIds []string) []*OnlineStatusResult {
    results := make([]*OnlineStatusResult, 0, len(userIds))
    for _, userId := range userIds {
        result := &OnlineStatusResult{
            UserId: userId,
            Status: constant.StatusOffline,
        }
        details := make([]*PlatformStatusDetail, 0)

        // 1. 收集本地连接
        clients, ok := s.userMap.GetAll(userId)
        if ok && len(clients) > 0 {
            for _, c := range clients {
                details = append(details, &PlatformStatusDetail{
                    PlatformId:   c.PlatformId,
                    PlatformName: constant.PlatformIdToName(c.PlatformId),
                    ConnId:       c.ConnId,
                })
            }
        }

        // 2. 收集远端连接（排除本实例，避免重复）
        if s.routeStore != nil {
            rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
            refs, err := s.routeStore.GetUserConnRefs(rctx, userId)
            cancel()
            if err == nil {
                for _, ref := range refs {
                    if ref.InstanceId == s.instanceId {
                        continue // 本实例连接已从 UserMap 获取，跳过
                    }
                    details = append(details, &PlatformStatusDetail{
                        PlatformId:   ref.PlatformId,
                        PlatformName: constant.PlatformIdToName(ref.PlatformId),
                        ConnId:       ref.ConnId,
                    })
                }
            }
        }

        // 3. 汇总结果
        if len(details) > 0 {
            result.Status = constant.StatusOnline
            result.DetailPlatformStatus = details
        }
        results = append(results, result)
    }
    return results
}
```

---

## 配置说明

### config.yaml 新增配置

```yaml
websocket:
  # ... 现有配置保持不变 ...

  # 跨实例基础设施（新增）
  cross_instance:
    enabled: true                          # 是否启用跨实例推送基础设施
    instance_id: ""                        # 实例 ID，空则自动生成
    heartbeat_second: 10                   # 心跳间隔（秒）

  # 混合策略（新增，依赖 cross_instance.enabled=true）
  hybrid:
    enabled: true                          # 是否启用混合策略选择
    group_route_max_targets: 100           # 精准路由最大在线目标数
    group_route_max_instances: 4           # 精准路由最大涉及实例数
    force_broadcast_group_min_targets: 500 # 强制广播最小在线目标数
    route_ttl_seconds: 70                  # 路由表 TTL（秒）
    instance_alive_ttl_seconds: 30         # 实例心跳 TTL（秒）
    force_broadcast: false                 # true 时群聊全走广播（灰度用）
```

### 双开关灰度策略

| 阶段 | cross_instance.enabled | hybrid.enabled | hybrid.force_broadcast | 行为 |
|------|----------------------|----------------|----------------------|------|
| 0. 关闭 | false | - | - | 纯本地推送，与改造前一致 |
| 1. 基础设施验证 | true | false | - | 路由表和心跳运行，但群聊全广播，单聊走路由 |
| 2. 灰度广播 | true | true | true | 混合策略启用但群聊强制广播，验证广播链路 |
| 3. 全量混合 | true | true | false | 完整混合策略，按阈值自动选择 route/broadcast |

### 环境变量覆盖

```bash
NEXO_INSTANCE_ID=server1-8080              # 覆盖实例 ID
```

### Config 结构体新增

```go
type WebSocketCrossInstanceConfig struct {
    Enabled         bool   `mapstructure:"enabled"`
    InstanceId      string `mapstructure:"instance_id"`
    HeartbeatSecond int    `mapstructure:"heartbeat_second"`
}

type WebSocketHybridConfig struct {
    Enabled                       bool `mapstructure:"enabled"`
    GroupRouteMaxTargets          int  `mapstructure:"group_route_max_targets"`
    GroupRouteMaxInstances        int  `mapstructure:"group_route_max_instances"`
    ForceBroadcastGroupMinTargets int  `mapstructure:"force_broadcast_group_min_targets"`
    RouteTTLSeconds               int  `mapstructure:"route_ttl_seconds"`
    InstanceAliveTTLSeconds       int  `mapstructure:"instance_alive_ttl_seconds"`
    ForceBroadcast                bool `mapstructure:"force_broadcast"`
}

type WebSocketConfig struct {
    // ... 现有字段 ...
    CrossInstance WebSocketCrossInstanceConfig `mapstructure:"cross_instance"`
    Hybrid        WebSocketHybridConfig        `mapstructure:"hybrid"`
}
```

### 配置默认值

> **v1.2 新增:** 在 `config.go` 的 `Load()` 函数中补充新增配置项的默认值设置，与现有风格一致。

```go
// config.go Load() 中新增默认值
if cfg.WebSocket.CrossInstance.HeartbeatSecond == 0 {
    cfg.WebSocket.CrossInstance.HeartbeatSecond = 10
}
if cfg.WebSocket.Hybrid.GroupRouteMaxTargets == 0 {
    cfg.WebSocket.Hybrid.GroupRouteMaxTargets = 100
}
if cfg.WebSocket.Hybrid.GroupRouteMaxInstances == 0 {
    cfg.WebSocket.Hybrid.GroupRouteMaxInstances = 4
}
if cfg.WebSocket.Hybrid.ForceBroadcastGroupMinTargets == 0 {
    cfg.WebSocket.Hybrid.ForceBroadcastGroupMinTargets = 500
}
if cfg.WebSocket.Hybrid.RouteTTLSeconds == 0 {
    cfg.WebSocket.Hybrid.RouteTTLSeconds = 70
}
if cfg.WebSocket.Hybrid.InstanceAliveTTLSeconds == 0 {
    cfg.WebSocket.Hybrid.InstanceAliveTTLSeconds = 30
}
```

### Redis Key 新增

> **v1.1 修正:** 统一命名风格，与现有 key（如 `nexo:online:{user_id}`）保持平铺式一致，
> 移除多余的 `ws:` 前缀。去重改为本地 sync.Map，移除 redisKeyPushDedup。

```go
// pkg/constant/constant.go 新增
const (
    redisKeyUserRoute      = "route:user:%s"       // route:user:{user_id}
    redisKeyInstanceAlive  = "instance:alive:%s"    // instance:alive:{instance_id}
    redisChannelInstPush   = "push:instance:%s"     // push:instance:{instance_id}
    redisChannelBroadPush  = "push:broadcast"       // push:broadcast
)

func RedisKeyUserRoute() string           { return redisKeyPrefix + redisKeyUserRoute }
func RedisKeyInstanceAlive() string       { return redisKeyPrefix + redisKeyInstanceAlive }
func RedisChannelInstancePush() string    { return redisKeyPrefix + redisChannelInstPush }
func RedisChannelBroadcastPush() string   { return redisKeyPrefix + redisChannelBroadPush }
```

---

## 错误处理与降级

### 降级策略

| 场景 | 行为 |
|------|------|
| `cross_instance.enabled=false` | 完全回退到本地推送，零开销，不初始化任何跨实例组件 |
| `hybrid.force_broadcast=true` | 群聊全走广播，单聊仍走路由（灰度安全网） |
| Redis 启动时不可用 | 跨实例功能不启动，本地推送正常，日志告警 |
| Redis 运行时断开 | 依赖 go-redis/v9 内置 PubSub 自动重连，持久性错误时降级为本地推送 |
| 路由表查询失败 | 回退到广播模式（宁可多发不漏发） |
| Pub/Sub 发布失败 | 记录日志，不阻塞消息发送 API，客户端通过 pull 补偿 |
| 信封反序列化失败 | 丢弃单条消息，记录日志 |

### 消息可靠性保证

消息推送是"尽力而为"，可靠性由以下机制保证：

1. **DB 持久化**：消息先写入 MySQL，成功后才触发推送
2. **Seq 机制**：客户端通过 `GetNewestSeq` 检测 seq 不连续时主动 pull
3. **推送失败不回滚**：推送失败不影响消息已发送的事实
4. **任何 Redis 失败不能导致 `SendMessage` 返回失败**

### 消息去重

广播模式下同一消息被所有实例收到。使用本地 `sync.Map` 做实例级去重（key = `pushID`，value = 写入时间戳），由单个 goroutine 每秒定期批量清理超过 5s 的条目（`startDedupCleaner`）。不依赖 Redis SETNX，减少一次网络往返；不为每条消息启动单独的清理 goroutine，避免高频推送时 goroutine 累积。

### 路由表一致性

- 路由表使用 TTL（70s）自动过期，防止实例宕机后残留
- 连接注册和注销均为异步 goroutine + 带超时 context，不阻塞 eventLoop
- 心跳 TTL（30s）< 路由 TTL（70s），实例宕机后路由最多残留 70s
- **路由 TTL 续期：** InstanceManager 心跳回调中通过 `RouteStore.RefreshAllConnRoutes` 批量续期本实例所有本地连接的路由 TTL，防止长连接用户的路由在 70s 后过期丢失。续期逻辑：获取 `UserMap.GetAllOnlineUserIds()` → Pipeline 批量 EXPIRE

### 在线状态双源说明

> **v1.2 新增:** 现有 `UserMap` 维护 `nexo:online:{userId}` Redis key（60s TTL）用于简单的在线判断。
> 本方案新增 `RouteStore` 维护 `nexo:route:user:{userId}` Hash（70s TTL）用于连接级路由。
>
> **两者关系：**
> - `nexo:online:{userId}` → 现有逻辑保留不变，用于 `UserMap.IsOnline()` 的分布式检查
> - `nexo:route:user:{userId}` → 新增，用于跨实例推送的精准路由和 `GetUsersOnlineStatus` 的多实例聚合
> - 长期方向：当 RouteStore 稳定后，可考虑用 `HLEN nexo:route:user:{userId} > 0` 替代 `nexo:online:{userId}`，
>   统一为单一数据源。但本期不做此变更，避免影响现有 `UserMap.IsOnline()` 调用方。

### WsServer 生命周期管理

> **v1.2 新增:** WsServer 新增内部 `context.Context` + `context.CancelFunc`，
> 解决现有 `main.go` 传入 `context.TODO()` 导致 goroutine 无法被停止的问题。
> `Run()` 中创建内部 ctx，所有内部 goroutine（eventLoop、pushLoop、dedupCleaner）使用该 ctx。
> `Shutdown()` 中 cancel 内部 ctx，确保所有 goroutine 可被优雅停止。

```go
// WsServer 新增字段
type WsServer struct {
    // ... 现有字段 ...
    routeStore  *RouteStore
    pushBus     PushBus
    selector    *HybridSelector
    instanceId  string
    pushDedup   sync.Map

    // v1.2: 内部生命周期管理
    internalCtx    context.Context
    internalCancel context.CancelFunc
    instMgr        *InstanceManager
}

// Run 改造: 创建内部 ctx，所有 goroutine 使用内部 ctx
func (s *WsServer) Run() {
    s.internalCtx, s.internalCancel = context.WithCancel(context.Background())

    // 启动 eventLoop 和 pushLoop（使用内部 ctx）
    go s.eventLoop(s.internalCtx)
    workerNum := s.cfg.WebSocket.PushWorkerNum
    if workerNum <= 0 {
        workerNum = 10
    }
    for i := 0; i < workerNum; i++ {
        go s.pushLoop(s.internalCtx)
    }

    // 跨实例组件（条件启动）
    if s.cfg.WebSocket.CrossInstance.Enabled && s.pushBus != nil {
        s.pushBus.SubscribeInstance(s.internalCtx, s.instanceId, s.onRemoteEnvelope)
        s.pushBus.SubscribeBroadcast(s.internalCtx, s.onRemoteEnvelope)
        s.startDedupCleaner(s.internalCtx)
    }

    // 启动 InstanceManager（如有）
    if s.instMgr != nil {
        s.instMgr.Start(s.internalCtx)
    }
}
```

### 优雅关闭

```go
func (s *WsServer) Shutdown(ctx context.Context) error {
    // 1. 停止接受新连接（由 HTTP server shutdown 保证）

    // 2. 停止 InstanceManager 心跳（路由表 70s 后自动过期）
    if s.instMgr != nil {
        s.instMgr.Stop(ctx)
    }

    // 3. 关闭 PushBus 订阅（停止接收远端推送）
    if s.pushBus != nil {
        s.pushBus.Close()
    }

    // 4. 等待 pushChan 排空（最多 10s）
    drainCtx, drainCancel := context.WithTimeout(ctx, 10*time.Second)
    defer drainCancel()
    for {
        select {
        case task := <-s.pushChan:
            s.processPushTask(drainCtx, task)
        case <-drainCtx.Done():
            goto drained
        default:
            goto drained
        }
    }
drained:

    // 5. cancel 内部 ctx，停止所有内部 goroutine（eventLoop、pushLoop、dedupCleaner）
    if s.internalCancel != nil {
        s.internalCancel()
    }

    // 6. 关闭所有客户端连接（触发 UnregisterConn 异步清理路由）
    // （由 HTTP server shutdown 关闭底层连接触发）

    return nil
}
```

**main.go 集成:** `Run()` 不再需要外部 ctx 参数。在 `signal.Notify` 收到信号后，先调用 `wsServer.Shutdown(ctx)` 关闭跨实例组件和内部 goroutine，再调用 `h.Shutdown(ctx)` 关闭 HTTP 服务：

```go
// Run 不再需要传入 ctx
wsServer.Run()

// ... server startup ...

<-quit
log.CtxInfo(ctx, "shutting down server...")

// 先关闭 WsServer（内部 goroutine + 跨实例组件）
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
if err := wsServer.Shutdown(shutdownCtx); err != nil {
    log.CtxError(ctx, "ws server shutdown error: %v", err)
}

// 再关闭 HTTP 服务
if err = h.Shutdown(shutdownCtx); err != nil {
    log.CtxError(ctx, "http server shutdown error: %v", err)
}
```

### 风险闸门（每个实现阶段都要检查）

1. 任何 Redis 失败不能导致 `SendMessage` 返回失败
2. `ExcludeConnId` 逻辑不能被跨实例分发破坏
3. broadcast 路径必须按本地在线用户过滤，不可直接推给所有 conn
4. route TTL 与 heartbeat TTL 的默认值必须可配置，且支持回滚到单实例模式
5. 同一用户多端在不同实例时，所有连接都必须收到推送（连接级过滤，非用户级）
6. 路由 TTL 续期必须在心跳中运行，防止长连接用户路由过期
7. eventLoop 中不能有同步 Redis I/O，所有路由操作必须异步执行（v1.2 新增）
8. WsServer 所有内部 goroutine 必须通过 internalCtx 可被 Shutdown 停止（v1.2 新增）

### ExcludeConnId 现状说明

> 当前 `MessageService` 调用 `AsyncPushToUsers` 时 `excludeConnId` 始终传空字符串。
> ExcludeConnId 相关逻辑在本方案中完整实现以支持未来扩展，但当前不会被触发。
> 后续优化建议：在 WebSocket `HandleSendMsg` 中将发送者的 `client.ConnId` 透传到
> `SendMessage` → `AsyncPushToUsers`，避免发送者自己的连接收到自己发的消息推送。

---

## 改动范围

### 新增文件

| 文件 | 职责 |
|------|------|
| `internal/gateway/instance_manager.go` | 实例管理器（注册、心跳、关闭） |
| `internal/gateway/route_store.go` | 连接级路由存储（Redis Hash） |
| `internal/gateway/push_bus.go` | PushBus 接口 + RedisPushBus 实现 |
| `internal/gateway/push_protocol.go` | PushEnvelope、PushMode、ConnRef 定义 |
| `internal/gateway/push_selector.go` | HybridSelector 策略选择器 |
| `internal/gateway/instance_manager_test.go` | 单元测试 |
| `internal/gateway/route_store_test.go` | 单元测试 |
| `internal/gateway/push_bus_test.go` | 单元测试 |
| `internal/gateway/push_protocol_test.go` | 单元测试 |
| `internal/gateway/push_selector_test.go` | 单元测试 |
| `internal/gateway/ws_server_push_test.go` | processPushTask 改造测试 |
| `internal/gateway/ws_server_online_status_test.go` | 在线状态多实例聚合测试 |
| `tests/websocket_multi_instance_test.go` | 跨实例集成测试 |

### 修改文件

| 文件 | 改动内容 |
|------|---------|
| `internal/config/config.go` | 新增 `WebSocketCrossInstanceConfig`、`WebSocketHybridConfig` |
| `config/config.yaml` | 新增 `websocket.cross_instance` 和 `websocket.hybrid` 配置段 |
| `pkg/constant/constant.go` | 新增 4 个 Redis Key/Channel 模式 |
| `internal/gateway/ws_server.go` | 新增字段（routeStore/pushBus/selector/instanceId/pushDedup/internalCtx/internalCancel/instMgr），拆分 `processPushTask` 为三方法，`registerClient`/`unregisterClient` 新增异步 RouteStore 调用，`GetUsersOnlineStatus` 合并本地+远端，`Run` 改为无参数使用内部 ctx，新增 `Shutdown` 方法 |
| `cmd/server/main.go` | resolveInstanceId，初始化 InstanceManager、RouteStore、PushBus、HybridSelector，注入 WsServer，`Run()` 改为无参数，优雅关闭中调用 `wsServer.Shutdown` |

### 不改动

| 文件/模块 | 原因 |
|-----------|------|
| `internal/service/message_service.go` | `MessagePusher` 接口不变 |
| `internal/gateway/user_map.go` | UserMap 不改动，RouteStore 调用在 WsServer 层 |
| `internal/gateway/client.go` | Client 结构不变 |
| `internal/gateway/client_conn.go` | 连接层不变 |
| `internal/gateway/protocol.go` | WebSocket 协议不变 |
| 所有 HTTP API handler | 不涉及 |

---

## 测试策略

### 单元测试（miniredis，不依赖外部服务）

**InstanceManager:**
- 注册后 Redis 中存在实例信息
- 心跳刷新 TTL
- 心跳回调（onHeartbeat）被正确调用
- Stop 后清理注册

**RouteStore:**
- RegisterConn → HSET 写入正确，值格式 `instanceId|connId|platformId`
- UnregisterConn → HDEL 删除正确
- GetUserConnRefs → 返回所有连接路由并正确解析
- GetUsersConnRefs → Pipeline 批量查询
- RefreshAllConnRoutes → Pipeline 批量续期 TTL
- TTL 过期后路由自动消失

**PushBus:**
- PublishToInstance → 消息到达实例 channel
- PublishBroadcast → 消息到达广播 channel
- Subscribe handler 正确反序列化 PushEnvelope

**HybridSelector:**
- 单聊 → 始终 Route
- ForceBroadcast=true → 群聊始终 Broadcast
- 小群在线数 ≤ GroupRouteMaxTargets → Route
- 大群在线数 ≥ ForceBroadcastGroupMinTargets → Broadcast
- 中间地带根据实例数判断

**WsServer (processPushTask):**
- 本地有连接 + 远端有路由 → 本地 PushMessage + PublishToInstance
- 同一用户在本地和远端都有连接 → 本地投递 + 远端连接通过 route/broadcast 投递（连接级过滤）
- Selector 返回 broadcast → PublishBroadcast 调用一次
- Route 模式 → 每个目标实例收到独立信封（只含自己的 ConnRef）
- cross_instance.enabled=false → 不触发跨实例逻辑
- 路由查询失败 → fallbackBroadcast 回退广播

**WsServer (GetUsersOnlineStatus):**
- 本地无连接但 RouteStore 有路由 → 返回 online（含远端连接详情）
- 本地有连接 + 远端也有连接 → 合并两者，全部返回
- RouteStore 查询失败 → 仅返回本地结果（不报错）
- RouteStore 查询超时（2s） → 仅返回本地结果

**WsServer (lifecycle):**
- `Run()` 后所有 goroutine 启动
- `Shutdown()` 后所有 goroutine 停止（internalCtx cancelled）
- `Shutdown()` 排空 pushChan 中的剩余任务
- registerClient/unregisterClient 路由操作不阻塞 eventLoop（异步 + 3s 超时）

### 集成测试

**跨实例单聊 (Route):**
- 启动实例 A + B（共享 Redis/MySQL，不同 instance_id 和端口）
- 用户 1 连接 A，用户 2 连接 B
- 用户 1 发消息给用户 2
- 验证用户 2 通过 WebSocket 实时收到推送

**跨实例群聊 (Broadcast):**
- 大群场景触发 broadcast
- 验证 A、B 实例上的在线成员都收到推送

**降级验证:**
- `cross_instance.enabled=false` 时跨实例推送不触发
- 现有单实例测试全部通过（auth/group/message/conversation/websocket）

---

## 验收标准

### 功能验收

- [ ] 单聊消息同实例推送正常（回归）
- [ ] 单聊消息跨实例推送正常（Route 模式）
- [ ] 小群消息跨实例推送正常（Route 模式）
- [ ] 大群消息跨实例推送正常（Broadcast 模式）
- [ ] 同一用户多端在不同实例，都能收到推送
- [ ] ExcludeConnId 正确排除发送者连接（本地 + 跨实例）
- [ ] 消息去重生效（广播模式不重复推送）
- [ ] `cross_instance.enabled=false` 完全回退，无副作用
- [ ] `hybrid.force_broadcast=true` 群聊全走广播
- [ ] 在线状态查询合并本地+远端连接结果
- [ ] 实例优雅关闭后路由表自动过期
- [ ] Shutdown 后所有内部 goroutine 停止（无泄漏）
- [ ] eventLoop 中无同步 Redis I/O（路由注册/注销均异步）

### 性能验收

- [ ] 单聊跨实例延迟 < 100ms
- [ ] 大群广播延迟 < 200ms
- [ ] 路由表批量查询（100 用户）< 10ms
- [ ] 内存增量 < 50MB（10000 连接）

### 测试验收

- [ ] 单元测试覆盖率 ≥ 80%
- [ ] 集成测试全部通过
- [ ] 现有测试套件全部通过（无回归）

---

## 后续优化方向（不在本期范围）

1. **Redis Stream 替代 Pub/Sub** — 支持消息持久化和重放
2. **实例分组** — 按地域/业务分组，减少广播范围
3. **Prometheus 指标** — push_route_total、push_broadcast_total、push_deliver_latency 等
4. **消息批处理** — 合并短时间内同一实例的多条推送，减少 Pub/Sub 调用
