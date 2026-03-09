# 多实例精准路由推送一期收敛方案

**文档版本:** 1.6.0
**创建日期:** 2026-03-03
**修订日期:** 2026-03-08
**状态:** 收敛修订版

## 修订记录

| 版本 | 日期 | 变更摘要 |
|------|------|---------|
| 1.0 - 1.5.7 | 2026-03-03 ~ 2026-03-06 | 历史迭代版本，围绕多实例混合推送、生命周期门禁、路由一致性与在线状态语义持续修订 |
| 1.6.0 | 2026-03-08 | 基于当前代码架构重新收敛为一期落地版：本期只做 `lifecycle + route-only + presence 聚合`，移除本期 `broadcast/hybrid`、过细 capability 与过重编排，明确 gateway/route/presence/lifecycle 边界 |
| 1.6.1 | 2026-03-08 | 基于当前代码现状补充落地约束：一期以最小侵入方式在现有 `WsServer` 周围接入 `LifecycleGate / PresenceService / PushCoordinator`，补齐 HTTP/WS send 与 WS 握手门禁、在线状态兼容结构、register 最终收敛与 `KickOnline` 一期语义边界 |

---

## 1. 概述

### 1.1 背景

当前 `nexo_v2` 的实时推送仍然是**单实例本地推送模型**：

- `MessageService.SendMessage()` 在消息写库后调用 `MessagePusher.AsyncPushToUsers(...)`
- `WsServer.processPushTask()` 仅查询本地 `UserMap`
- `UserMap` 当前同时维护本地连接表与 Redis online key
- `/user/get_users_online_status` 本质上仍是本机视角，而不是全局聚合视角
- 进程关闭时只有 HTTP server shutdown，没有 `WsServer` 级 drain / queue 收敛 / client 迁移流程

这套模型在单实例下可用，但在多实例部署下存在明确缺口：

1. 远端实例上的在线用户无法实时收到消息
2. 在线状态不是全局真实在线，而是“本地连接 + 不可靠 Redis 标记”的混合结果
3. 关闭期间缺少统一的发送门禁，容易出现“消息已写库，但推送入口关闭”的竞态
4. `WsServer` / `UserMap` 的职责边界已经开始膨胀，不适合直接叠加更重的广播与状态机设计

### 1.2 本期目标

在**不改消息写库主链路**、**不改对外 WebSocket 协议**、**不改 `MessageService` 业务语义**的前提下，先完成多实例一期能力：

- 单聊跨实例精准路由
- 小群跨实例精准路由
- 在线状态由“本地视角”升级为“本地连接 + 远端 presence 聚合”
- 引入 `LifecycleGate + SendLease`，解决关闭期发送竞态
- 引入统一 `drain-exit` 骨架，保证实例先停入口、再排空发送、再迁移连接、最后摘实例

### 1.3 本期明确不做

为贴合当前代码基座，本期**不纳入**以下能力：

- broadcast 推送
- route/broadcast 自动切换（hybrid selector）
- `broadcast_ready` 状态位
- 广播分片、广播 dedup、广播自投递保护
- `BroadcastSubscribe` 独立 capability
- 以“群成员数阈值”触发的自动广播优化
- 任何未知目标实例集合下的广播兜底

这些能力统一后置到二期优化阶段。

### 1.4 约束与边界

- 当前仓库的 seq 分配仍依赖 Redis；Redis 启动不可用时服务仍会启动失败，本期不处理
- 本期不改消息表、会话表、seq 表结构
- 本期不改 `MessageService` 业务接口，只增强 gateway 推送路径与发送门禁
- 群成员枚举仍由 `MessageService -> GroupRepo` 全量查询，本期不优化上游 fanout 成本
- 实时链路仍是“消息先写库，再尽力实时推送，最终由 seq + pull 补偿”，不是严格送达总线
- 优雅关闭的目标是**收敛竞态窗口与降低丢失概率**，不是承诺零损失

---

## 2. 设计结论

本期采用**保守收敛版 route-only 方案**：

- 实时推送只做**精准路由**，不做广播
- 先把生命周期、发送门禁、route store、presence 聚合做稳
- 一期以**最小侵入方式**在现有 `WsServer` 周围接入 `LifecycleGate / PresenceService / PushCoordinator`，而不是把 `WsServer` 一次性重构成极薄外壳
- `WsServer` 本期仍保留 gateway runtime core 职责，但不再继续承担在线聚合、跨实例编排与关闭流程控制的唯一真相源
- 跨实例推送编排、在线状态聚合、关闭流程控制分别落到独立组件/接口上
- 一期仍由 `WsServer` 作为 `MessagePusher` 对外注册身份，`PushCoordinator` 先作为其内部协作组件存在，避免同时改大 `main.go`、`MessageService.SetPusher(...)` 与现有 WS runtime 装配路径

一句话概括：

> **一期只做 correctness，不做成本优化。**

---

## 3. 分期策略

### 3.1 一期（本期）

一期只交付以下能力：

1. `LifecycleGate + SendLease`
2. `RouteStore + InstanceManager`
3. route-only 跨实例推送
4. `PresenceService` 聚合在线状态
5. `planned_upgrade / manual / subscribe_fault` 统一 `drain-exit` 骨架

### 3.2 二期（后续优化）

二期再评估是否引入：

1. broadcast 通道
2. hybrid selector
3. 广播分片与 dedup
4. 更细粒度 capability 控制面
5. 更激进的 stale route 清理优化

二期必须建立在一期已经稳定运行的前提上，而不是与一期并行推进。

---

## 4. 核心设计决策

| 决策项 | 选择 | 说明 |
|--------|------|------|
| 系统边界 | 一期改动集中在 gateway / handler / router / main | 保持消息写库主链路与外部协议不变 |
| 推送模式 | route-only | 只做精准路由，不做本期 broadcast |
| 本地事实源 | `UserMap` | `UserMap` 只保留本地连接事实源职责 |
| 在线语义 | `online = socket connected` | presence 表示仍有活跃 socket，不等价于“仍可实时路由命中” |
| 在线查询 | `PresenceService` | handler 不再直接把 gateway 当成在线查询服务使用 |
| 分布式路由 | `RouteStore + InstanceRouteIndex` | 用户维度 hash 负责读路径，实例维度索引负责 reconcile 与 cleanup |
| 实例标识 | 进程唯一 `instance_id` | 每次进程启动生成新值，重启/升级不得复用 |
| 实例可路由模型 | `alive + ready + routeable + draining` | `ready` 只表示是否接新流量；`routeable` 表示是否仍能接收远端 push |
| 路由读取 | 只支持精准路由 | 本期不存在广播兜底 |
| presence 读取 | 与 push route 读路径分离 | push route 看 `alive=1 && routeable=1`；presence 看 `alive=1` |
| 注册可靠性 | 高可靠 + 最终收敛，不阻塞 eventLoop | 通过 route writer / repair / reconcile 避免因队列满形成永久漏写，不承诺进程崩溃场景下的同步线性保证 |
| 注销可靠性 | 弱保证 + 周期修复 | 依赖 `routeable` 过滤、TTL、索引 cleanup 收敛 |
| 发送门禁 | `LifecycleGate + SendLease` | 防止“已写库但因为关闭而未提交 push” |
| 关闭语义 | 先停 ingress，再停新 send，再迁连接，最后摘实例 | 避免旧连接仍在线时提前从路由层消失 |
| 故障处置 | `drain-exit` | `planned_upgrade / manual / subscribe_fault` 共用一套排空退出骨架 |
| 降级原则 | 不扩大影响面 | route 不可读时统一 local only + pull 补偿，不做广播兜底 |

---

## 5. 架构分层

### 5.1 总体结构

```text
MessageService.SendMessage()
    -> MessagePusher.AsyncPushToUsers(msg, userIds, excludeConnId)
        -> PushCoordinator.AsyncPushToUsers(...)
            -> pushChan -> processPushTask()
                1. pushToLocalClients()
                2. if cross-instance publishable:
                       dispatchRouteOnly()

跨实例基础设施：
    WsServer
      |- UserMap                  本地连接事实源
      |- Client / ClientConn      连接生命周期与读写循环

    PushCoordinator
      |- LifecycleGate            ingress/send/drain 门禁
      |- RouteStore               路由读写与校准
      |- InstanceManager          instance alive/ready/routeable 镜像
      |- PushBus                  实例定向 route publish/subscribe

    PresenceService
      |- UserMap                  本地连接视图
      |- RouteStore               远端 presence 聚合
```

### 5.2 分层原则

1. `WsServer` 本期主要负责：
   - WS 握手与 ingress 门禁
   - client 注册/注销
   - 本地连接推送执行
   - 继续承载现有 eventLoop / push worker runtime 壳
   - client drain/kick 协助

   说明：
   - 一期不要求把 `WsServer` 彻底降成极薄壳
   - 一期要求的是把在线聚合、跨实例编排、生命周期控制从 `WsServer` 中逐步解耦出去

2. `PushCoordinator` 负责：
   - `processPushTask`
   - local push 之后是否做跨实例 route publish
   - route 读取失败时的降级决策
   - remote route envelope 消费

3. `PresenceService` 负责：
   - 在线状态聚合
   - 降级为 `local_only` 时的 `response.meta`
   - 返回独立于 `gateway` 的 online-status DTO；一期不要让 `service` 反向依赖 `gateway/ws_server.go` 中的结构定义

4. `LifecycleGate` 负责：
   - 统一 ingress/send/drain 门禁
   - 对 HTTP `/msg/send`、WS `WSSendMsg`、WS 握手 `/ws`、`AsyncPushToUsers()` 暴露统一关闭语义
   - `SendLease` 必须在进入 `MessageService.SendMessage()` 前获取；`AsyncPushToUsers()` 上的 gate 只作为二次保护，不作为主保证

5. handler 层只依赖接口，不直接依赖 `WsServer` 内部状态。

---

## 6. 组件职责

### 6.1 WsServer

职责：

- 管理 WebSocket 连接接入
- 持有 `UserMap`
- 负责本地 `register/unregister`
- 提供本地 client push 执行能力
- 在 drain 期间配合执行批量 `KickOnline/Close`

非职责：

- 不直接承担跨实例 route 查询
- 不直接承担 presence 聚合
- 不直接维护复杂 capability 控制面
- 不再扩张为整个多实例系统的唯一编排器

### 6.2 UserMap

职责：

- 仅维护本地连接事实源
- 提供：
  - `GetAll(userId)`
  - `HasConnection(userId)`
  - `SnapshotRouteConns()`
  - `SnapshotOnlineUsers()`

非职责：

- 不再直接写 Redis online key
- 不再承担分布式在线状态真相源职责
- 不再承担跨实例路由职责

### 6.3 RouteStore

职责：

- 保存用户维度路由：`nexo:route:user:{userId}`
- 保存实例拥有索引：`nexo:route:inst:{instanceId}`
- 提供 register / unregister / query / ttl refresh / reconcile
- 提供两类读路径：
  - PushRouteRead：过滤 `alive=1 && routeable=1`
  - PresenceRead：过滤 `alive=1`

### 6.4 InstanceManager

职责：

- 管理实例活跃态 `nexo:instance:alive:{instanceId}`
- 镜像本地 `ready / routeable / draining`
- 对外提供 `/ready` 语义支撑
- 为 route 过滤提供实例状态依据

本期不做：

- `broadcast_ready`
- 广播链路健康位镜像

### 6.5 PushBus

职责：

- 只负责**实例定向 route 通道**的 publish / subscribe
- 运行时错误回调给 route 订阅故障处理器

本期不做：

- 广播通道
- 广播 envelope 协议

### 6.6 PresenceService

职责：

- 聚合本地连接 + 远端 presence
- 对 `/user/get_users_online_status` 提供统一查询接口
- 兼容当前在线状态响应结构（包括平台明细），避免因为服务分层调整而改变对外返回语义
- 远端查询失败时返回 `response.meta.partial=true` 与 `data_source=local_only`

### 6.7 LifecycleGate

职责：

- 统一管理：
  - `ready`
  - `routeable`
  - `ingressClosed`
  - `sendDraining`
  - `sendClosed`
  - `draining`
- 提供 `SendLease`
- 统一输出关闭期公共错误语义

---

## 7. 数据结构

### 7.1 路由结构

```go
type RouteConn struct {
    UserId     string
    ConnId     string
    InstanceId string
    PlatformId int
}
```

Redis 存储：

```text
Key:  nexo:route:user:{user_id}
Type: Hash
Field: {conn_id}
Value: "{instance_id}|{platform_id}"
TTL: 70s

Key:  nexo:route:inst:{instance_id}
Type: Set
Member: "{user_id}|{conn_id}"
TTL: 70s
```

设计说明：

- `user route hash` 负责读路径
- `instance route index` 负责 authoritative reconcile 与 stale cleanup
- `register / unregister / ttl refresh` 应尽量同轮维护两套索引
- TTL 只做兜底，不替代显式注销
- `instance_id` 必须是进程唯一，重启和滚动升级后不得复用

### 7.2 实例活跃结构

```text
Key:  nexo:instance:alive:{instance_id}
Type: Hash
TTL: 30s
```

示例：

```json
{
  "instance_id": "server1-8080-a1b2c3",
  "ready": "1",
  "routeable": "1",
  "draining": "0",
  "started_at": "1709366400000",
  "last_heartbeat": "1709366430000",
  "conn_count": "1234"
}
```

字段语义：

- key 存在表示 `alive=1`
- `ready=1` 表示可接新 HTTP/WS 流量
- `routeable=1` 表示仍可被其他实例作为 route-only push 目标
- `draining=1` 表示实例进入排空
- `ready=0 && routeable=1` 是 planned/manual drain 的合法中间态
- `ready=0 && routeable=0` 表示该实例已从实时路由层摘除

### 7.3 生命周期门禁

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

要求：

- `LifecycleGate` 是本地生命周期状态的唯一真相源
- `InstanceManager` 只负责把本地快照镜像到 Redis
- 本期不提供 reopen / recover to ready 语义；进入 drain 的当前进程终态是退出
- HTTP `/msg/send`、WS `WSSendMsg` 与 `AsyncPushToUsers()` 必须共用同一门禁来源

### 7.4 route-only 推送协议

```go
type PushMode string

const (
    PushModeRoute PushMode = "route"
)

type PushContent struct {
    Text   string `json:"text,omitempty"`
    Image  string `json:"image,omitempty"`
    Video  string `json:"video,omitempty"`
    Audio  string `json:"audio,omitempty"`
    File   string `json:"file,omitempty"`
    Custom string `json:"custom,omitempty"`
}

type PushPayload struct {
    MsgId          int64       `json:"msg_id"`
    ConversationId string      `json:"conversation_id"`
    Seq            int64       `json:"seq"`
    ClientMsgId    string      `json:"client_msg_id"`
    SenderId       string      `json:"sender_id"`
    RecvId         string      `json:"recv_id,omitempty"`
    GroupId        string      `json:"group_id,omitempty"`
    SessionType    int32       `json:"session_type"`
    MsgType        int32       `json:"msg_type"`
    Content        PushContent `json:"content"`
    SendAt         int64       `json:"send_at"`
}

type ConnRef struct {
    UserId     string `json:"user_id"`
    ConnId     string `json:"conn_id"`
    PlatformId int    `json:"platform_id"`
}

type PushEnvelope struct {
    PushId         string               `json:"push_id"`
    Mode           PushMode             `json:"mode"`
    TargetConnMap  map[string][]ConnRef `json:"target_conn_map,omitempty"`
    SourceInstance string               `json:"source_instance"`
    SentAt         int64                `json:"sent_at"`
    Payload        *PushPayload         `json:"payload"`
}
```

要求：

- 本期 envelope 只支持 `route`
- 每个目标实例一份独立 envelope
- 接收端零查询，直接根据 `TargetConnMap[self]` 投递
- 不引入广播字段，避免协议提前膨胀

---

## 8. 推送主流程

### 8.1 `processPushTask`

```go
func (c *PushCoordinator) processPushTask(ctx context.Context, task *PushTask) {
    payload := c.toPushPayload(task.Msg)
    msgData := c.toMessageData(payload)

    c.pushToLocalClients(ctx, task, msgData)

    if !c.publishReady() || c.routeStore == nil || c.pushBus == nil {
        return
    }

    c.dispatchRouteOnly(ctx, task, payload)
}
```

规则：

- 本地推送始终优先
- 只有 route-only 发布能力可用时才尝试跨实例推送
- route 读取失败时统一退化为 `local only + pull 补偿`
- 本期不存在广播兜底

### 8.2 route-only 分发

```go
func (c *PushCoordinator) dispatchRouteOnly(ctx context.Context, task *PushTask, payload *PushPayload) {
    routeMap, err := c.routeStore.GetUsersConnRefs(ctx, task.TargetIds)
    if err != nil {
        c.handleRouteReadUnavailable(ctx, task, payload, err)
        return
    }

    remoteRouteMap := filterOutLocalInstance(routeMap, c.instanceId)
    if len(remoteRouteMap) == 0 {
        return
    }

    grouped := groupByInstance(remoteRouteMap)
    for instId, refs := range grouped {
        env := &PushEnvelope{
            PushId:         uuid.New().String(),
            Mode:           PushModeRoute,
            TargetConnMap:  map[string][]ConnRef{instId: refs},
            SourceInstance: c.instanceId,
            SentAt:         time.Now().UnixMilli(),
            Payload:        payload,
        }
        if err := c.pushBus.PublishToInstance(ctx, instId, env); err != nil {
            c.onPublishFailure(err)
        } else {
            c.onPublishSuccess()
        }
    }
}
```

### 8.3 route 不可用时的降级

规则：

1. 单聊：`local only + pull 补偿`
2. 群聊：`local only + pull 补偿`
3. 不因 route 读取失败改走广播
4. presence 查询失败不应影响消息主链路

原因：

- sender 在不知道目标实例集合时，无法证明广播是安全的
- 一期目标是不扩大影响面，而不是“任何故障都宁可多发”

### 8.4 远端 route envelope 处理

```go
func (c *PushCoordinator) onRemoteEnvelope(ctx context.Context, env *PushEnvelope) {
    msgData := c.toMessageData(env.Payload)
    deliverRouteEnvelopeLocally(msgData, env.TargetConnMap[c.instanceId])
}
```

要求：

- 接收端不再查询 Redis 路由
- 只按 envelope 里给出的本实例连接投递
- route 订阅错误视为实例级故障，触发 `subscribe_fault -> drain-exit`

---

## 9. 路由写入与一致性

### 9.1 原则

- `register` 丢失会造成长期漏路由，必须通过高可靠写入路径避免形成永久漏路由
- `unregister` 丢失会形成 stale route，可通过过滤与校准收敛
- reconcile 是长期一致性兜底，不替代 register 正常写入

### 9.2 注册：高可靠写入 + 最终收敛，但不阻塞 eventLoop

流程：

1. `UserMap.Register()` 先更新本地事实源
2. 尝试把 register 事件写入 `routeWriteChan`
3. 若队列满：写入 `pendingRegisterMap` / `mustRegisterSet`
4. 由独立 `routeRepairWorker` 进行短超时重试
5. 周期 reconcile 最终兜底

约束：

- 不能在 `WsServer.eventLoop` 里直接做同步 Redis I/O
- 不能直接因为队列满而永久丢 register
- 进程正常退出前应 best-effort flush pending register；若来不及完成，最终仍依赖 reconcile 收敛

### 9.3 注销：弱保证 + 周期收敛

流程：

1. `UserMap.Unregister()` 先更新本地事实源
2. 异步写 `RouteStore.UnregisterConn()`
3. 队列满时允许退化为 best-effort
4. 依赖 `routeable` 过滤、TTL、reconcile 与 dual-index cleanup 收敛 stale route

### 9.4 周期校准

新增：

```go
func (m *UserMap) SnapshotRouteConns() []RouteConn
func (s *RouteStore) ReconcileInstanceRoutes(ctx context.Context, instanceId string, conns []RouteConn) error
```

校准策略：

- `InstanceManager` 心跳只刷新 `instance_alive`
- `RouteTTLRefresher` 独立刷新 user hash TTL 与 instance index TTL
- 每隔 `route_reconcile_interval_seconds` 做一次全量 reconcile
- reconcile 读取 `nexo:route:inst:{instance_id}` 做 authoritative diff：
  - 补齐缺失 route
  - 删除 stale member
  - 回收 user hash 上的 stale field

### 9.5 推送路由读取

规则：

1. 批量查询 `GetUsersConnRefs(userIds...)`
2. 只保留 `alive=1 && routeable=1` 的实例
3. 惰性清理明确 stale 的 route
4. `ready=0 && routeable=1` 的 planned/manual drain 中间态**不能**被误删

---

## 10. 在线状态设计

### 10.1 核心语义

本期统一定义：

> `online = 用户当前仍有 socket 连接`

这不等价于：

- 该连接一定还能接收实时 push
- 该实例当前一定 `routeable=1`
- 该实例当前一定 `ready=1`

### 10.2 双源模型

本期仍保留双源，但职责明确：

- `nexo:online:{userId}`：仅作为历史兼容在线标记
- `RouteStore presence 读路径`：跨实例在线状态聚合的真实数据来源

要求：

- legacy online key 不参与 correctness 决策
- legacy online key 不能作为 route 过滤依据
- `UserMap` 不再直接写 online key，由 `OnlineStateWriter` 异步维护兼容逻辑

### 10.3 PresenceService

建议新增接口：

```go
type PresenceService interface {
    GetUsersOnlineStatus(ctx context.Context, userIds []string) ([]*OnlineStatusResult, *response.Meta, error)
}
```

其中 `OnlineStatusResult` / `PlatformStatusDetail` 应定义在不依赖 `gateway/ws_server.go` 的公共 DTO 位置，一期不要让 `service` 反向依赖 gateway 结构。

职责：

- 查询本地连接
- 查询远端 presence
- 聚合为统一在线状态结果
- 在远端查询失败时返回 `partial/local_only`

### 10.4 `GetUsersOnlineStatus`

规则：

- 单次限制 `user_ids <= 100`
- 先查本地连接，再查远端 presence
- 远端 presence 只过滤 `alive=1`
- `routeable=0`、`draining=1` 不应直接把用户判成离线
- 远端失败允许退化为本地结果，但必须返回：
- 返回结构需兼容当前 `status + detail_platform_status[]` 语义，不能因为改为 `PresenceService` 就丢失平台维度信息

```json
{
  "meta": {
    "partial": true,
    "data_source": "local_only"
  }
}
```

- 不能把“远端未知”伪装成“远端离线”

---

## 11. 生命周期与关闭语义

### 11.1 为什么必须引入 `LifecycleGate + SendLease`

当前系统最大的关闭期风险不是“会不会多发”，而是：

- 请求已经进入 `MessageService.SendMessage()`
- 消息已写库成功
- `AsyncPushToUsers()` 因 gate 翻转而拒绝
- 最终形成“写库成功但推送未提交”的不透明竞态

因此必须引入：

1. `AcquireSendLease()`：发送入口在写库前获取租约
2. `BeginSendDrain()`：停止发放新租约
3. `CloseSendPath()`：在 inflight lease 归零后硬关闭发送入口

本期必须覆盖的发送/接入入口：

- HTTP `/msg/send`
- WS `WSSendMsg`
- WS 握手 `/ws`
- `AsyncPushToUsers()`（二次保护）

### 11.2 `/health` 与 `/ready`

规则：

- `/health`：只表示进程仍活着
- `/ready`：表示是否还能接新流量
- route 过滤必须看 `routeable`，不能看 `/ready`
- planned/manual drain 期间允许 `ready=0 && routeable=1`
- subscribe_fault 进入 drain 时应立即 `ready=0 && routeable=0`
- 关闭期错误语义要统一：HTTP `/msg/send` 返回 503 + 统一 errcode；WS `WSSendMsg` 返回同一业务错误码；WS 握手 `/ws` 在 upgrade 前直接返回 HTTP 503

### 11.3 `drain-exit` 时序

#### planned_upgrade / manual

1. `MarkUnready() + CloseIngress() + BeginSendDrain() + EnterDraining()`
2. 停止 ingress 后等待 inflight `SendLease` 收敛
3. 收敛后执行 `CloseSendPath()`
4. 保持 `alive=1 + routeable=1` 一段有限 grace window
5. 分批 `KickOnline()` 迁移旧连接
6. 本地连接归零或超时后执行 `MarkUnrouteable()`
7. 排空队列后退出进程

#### subscribe_fault

1. `MarkUnready() + MarkUnrouteable() + CloseIngress() + BeginSendDrain() + EnterDraining()`
2. 等待 inflight `SendLease` 收敛
3. 执行 `CloseSendPath()`
4. 分批 `KickOnline()/Close()` 迁移旧连接
5. 排空队列后退出，不做 in-place recover

### 11.4 关闭顺序

```text
1. MarkUnready()             `/ready` 立即失败
2. CloseIngress()            拒绝新连接/新请求
3. BeginSendDrain()          拒绝新的 SendLease
4. 停止 Hertz ingress        不再接外部新流量
5. 等待 inflight send lease
6. CloseSendPath()           硬关闭 WSSendMsg / HTTP / AsyncPushToUsers
7. planned/manual 保持短暂 routeable=1
8. 保持 instance alive，继续排空 route/push 队列
9. 分批 Kick/Close 现有连接
10. 本地连接归零或超时后 MarkUnrouteable()
11. 停止 InstanceManager      摘除 instance_alive
12. 停止 PushBus              停止 route 订阅
13. cancel internal ctx       退出各 worker loop
14. WaitGroup 收敛            保证无 goroutine 泄露
```

### 11.5 `KickOnline` 语义

本期必须修正：

- `KickOnline` 不能只是把控制消息写入普通业务异步队列后立刻 close
- 一期至少要保证“先尝试写控制消息，再 close”，不能出现“先 close 后写控制消息”的反序
- 结合当前 `ClientConn` 模型，一期不承诺严格 flush-before-close 送达保证；控制通道优先级与显式 flush 机制后置到二期

---

## 12. 降级策略

一期只保留必要降级，不引入过细 capability 控制面。

### 12.1 推荐的一期能力态

```go
type CapabilityState int32

const (
    CapabilityDisabled CapabilityState = iota
    CapabilityReady
    CapabilityDegraded
)

type CrossInstanceCapabilities struct {
    Publish        atomic.Int32
    RouteSubscribe atomic.Int32
    PresenceRead   atomic.Int32
}
```

含义：

- `Publish`：是否允许向远端实例发布 route envelope
- `RouteSubscribe`：本实例是否仍可接收定向 route envelope；故障视为实例级故障
- `PresenceRead`：在线状态批量查询是否健康；故障不影响消息主链路

### 12.2 降级规则

| 场景 | 行为 |
|------|------|
| `cross_instance.enabled=false` | 纯本地推送 |
| `Publish=Degraded` | 停止 outbound 跨实例发布，仅保留本地推送 |
| `RouteSubscribe=Degraded` | 进入 `subscribe_fault -> drain-exit`，当前实例不再 routeable |
| `PresenceRead=Degraded` | 在线状态接口退化为本地结果 + `meta.partial=true` |
| route 读取失败 | `local only + pull 补偿` |

说明：

- 一期不单独引入 `PushRouteRead` capability；route 读取失败直接按本地降级处理
- 一期不引入 `BroadcastSubscribe`
- 一期不允许任何广播兜底

---

## 13. 配置设计

```yaml
websocket:
  push_channel_size: 10000
  push_worker_num: 10
  write_wait: 10s
  pong_wait: 30s
  ping_period: 27s
  write_channel_size: 256

  route_write_queue_size: 4096
  route_write_worker_num: 4
  route_stale_cleanup_limit: 64
  route_ttl_refresh_interval_seconds: 20
  route_reconcile_interval_seconds: 60

  cross_instance:
    enabled: false
    instance_id: ""
    heartbeat_second: 10
    route_ttl_seconds: 70
    instance_alive_ttl_seconds: 30
    publish_fail_threshold: 3
    route_subscribe_fail_threshold: 1
    presence_read_fail_threshold: 3
    recover_probe_interval_seconds: 5
    drain_timeout_seconds: 30
    drain_routeable_grace_seconds: 15
    drain_kick_batch_size: 200
    drain_kick_interval_ms: 100
```

说明：

- `cross_instance.enabled=false` 默认关闭，确保可回滚到单实例行为
- 当 `cross_instance.enabled=false` 时，`RouteStore / InstanceManager / PushBus` 应允许不启动或以 no-op / nil 形式装配，不能因为多实例组件初始化失败影响单实例启动路径
- `instance_id` 为空时进程启动自动生成
- `instance_id` 必须是进程唯一，不允许滚动升级复用
- 一期不保留 broadcast / hybrid 配置，避免“配置先行、实现缺席”
- 现有 `write_wait / pong_wait / ping_period / write_channel_size` 必须真实接入 `client_conn.go`

---

## 14. 改动范围

### 14.1 新增文件

- `internal/gateway/lifecycle_gate.go`
- `internal/gateway/route_store.go`
- `internal/gateway/instance_manager.go`
- `internal/gateway/push_bus.go`
- `internal/gateway/push_protocol.go`
- `internal/gateway/push_coordinator.go`
- `internal/gateway/online_state_writer.go`
- `internal/service/presence_service.go` 或等价目录
- 对应单元测试

### 14.2 修改文件

- `internal/gateway/ws_server.go`
  - 收敛为连接接入与本地推送执行
  - 对接 `PushCoordinator` / `LifecycleGate`
- `internal/gateway/user_map.go`
  - 去掉同步 Redis online 写入
  - 增加 `SnapshotRouteConns`
- `internal/gateway/client.go`
  - 修正 `KickOnline` 控制消息语义
- `internal/gateway/client_conn.go`
  - 真正接入 `write_wait / pong_wait / ping_period / write_channel_size`
- `internal/handler/message_handler.go`
  - 接入 `LifecycleGate`，将 HTTP `/msg/send` 纳入 `SendLease`
- `internal/handler/user_handler.go`
  - 改为依赖 `PresenceService`
  - 增加 `response.meta.partial`
- `internal/router/router.go`
  - 增加 `/ready`
- `internal/config/config.go`
  - 增加 route-only 跨实例配置
- `cmd/server/main.go`
  - 初始化 `instance_id`
  - 初始化 `PushCoordinator` / `PresenceService`
  - 增加 `WsServer` 级 shutdown 编排
- `pkg/errcode/errcode.go`
  - 增加关闭期统一错误码
- `pkg/response/response.go`
  - 增加 `SuccessWithMeta(...)` 或等价 helper

### 14.3 不改动

- `internal/service/message_service.go` 业务语义
- 对外 WebSocket 协议结构
- 消息表 / 会话表 / seq 表结构

---

## 15. 迁移顺序

### 步骤 0：底座与开关

- 增加 `cross_instance.enabled` 开关
- 增加自动生成 `instance_id`
- 保持默认关闭

### 步骤 1：先收敛生命周期

- 落地 `internalCtx`
- 落地 `LifecycleGate`
- 落地 `SendLease`
- 增加 `/ready`
- 修正 `main.go` 的 shutdown 顺序

### 步骤 2：先补齐生命周期入口闭环与基础 wiring

- HTTP `/msg/send` 接入 `SendLease`
- WS `WSSendMsg` 接入 `SendLease`
- WS 握手 `/ws` 接入 ingress gate
- `main.go` 先把 gate / readiness / shutdown 基础 wiring 接起来

### 步骤 3：拆出在线与路由职责

- 引入 `PresenceService`
- 将 `UserHandler` 从直接依赖 `WsServer` 改为依赖 presence 接口
- `UserMap` 去掉 Redis online 写路径
- 引入 `OnlineStateWriter`

### 步骤 4：实现 RouteStore 与实例状态

- 实现 register / unregister / query
- 实现 `instance_alive`
- 实现 route 过滤：`alive=1 && routeable=1`
- 实现 presence 过滤：`alive=1`

### 步骤 5：实现 route-only PushBus 与 PushCoordinator

- route 通道 publish / subscribe
- route envelope 协议
- local push 后的跨实例精准路由
- route read failure -> local only 降级

### 步骤 6：实现写入修复与 reconcile

- register repair worker
- TTL refresh
- authoritative reconcile
- stale cleanup

### 步骤 7：实现 drain-exit 与灰度

- `planned_upgrade / manual / subscribe_fault`
- 批量 `KickOnline` 迁移
- 验证 routeable grace window
- 小流量灰度开关

---

## 16. 测试策略

### 16.1 单元测试

#### LifecycleGate

- `BeginSendDrain()` 后拒绝新 lease
- 已获取 `SendLease` 的请求允许完成写库 + push 提交
- `CloseSendPath()` 只能在 inflight lease 归零后执行

#### RouteStore

- register 写入正确
- unregister 删除正确
- `alive=1 && routeable=1` 过滤正确
- `alive=1` 的 presence 读取正确
- `ready=0 && routeable=1` 的中间态不会被误判为 stale
- reconcile 可补齐漏写与清理 stale route

#### PushBus

- route channel 收发正确
- `Close()` 后 publish 报错
- 只有订阅主循环中断、无法持续消费等实例级故障才触发 `subscribe_fault`
- 单条坏消息 / 单次 decode 失败 / 单次 publish 失败不应直接触发实例 drain

#### PresenceService

- 本地 + 远端连接聚合正确
- 远端 presence 查询失败时返回 `meta.partial=true`
- `routeable=0` 但连接尚未收敛时仍返回在线
- 返回 DTO 不依赖 `gateway/ws_server.go` 中的 handler-facing 结构定义

#### WsServer / PushCoordinator

- 本地连接优先投递
- route-only 跨实例定向发布正确
- `excludeConnId` 在本地投递与远端分组前统一过滤，不因跨实例路由而失效
- route 读取失败时统一 local only
- `KickOnline` 一期至少满足先写控制消息再 close，不校验严格 flush 保证
- lifecycle 测试需分别覆盖 WS 握手拒绝、WS send lease 拒绝、`AsyncPushToUsers()` 关闭期行为
- shutdown 期间 `register/unregister` 不丢关键收敛事件

### 16.2 集成测试

- A/B 两实例单聊跨实例推送
- 小群 route-only 推送
- `planned_upgrade` 时旧实例先 `/ready` 失败，再在 grace window 内承接旧连接 push
- `subscribe_fault` 时实例立刻 `routeable=0` 并开始 drain
- HTTP `/msg/send` 在关闭期被拒绝
- `GetUsersOnlineStatus` 聚合本地 + 远端连接
- `PresenceRead` 降级时返回 `meta.partial=true`
- 滚动升级后新实例使用新 `instance_id`

### 16.3 回归测试

- 单实例 websocket 现有测试全部通过
- auth / group / message / conversation 不回归

---

## 17. 验收标准

### 功能验收

- [ ] 单聊跨实例 route-only 推送正常
- [ ] 小群跨实例 route-only 推送正常
- [ ] 同一用户多端跨实例全部收到消息
- [ ] `GetUsersOnlineStatus` 正确聚合本地和远端连接
- [ ] 远端 presence 查询失败时显式返回 `meta.partial=true`
- [ ] `cross_instance.enabled=false` 时行为与现网一致

### 生命周期验收

- [ ] `HandleSendMsg` 在关闭期被明确拒绝，而不是写库后静默不推
- [ ] HTTP `/msg/send` 在关闭期被明确拒绝，而不是写库后静默不推
- [ ] 已在关闭前获得 `SendLease` 的请求不会因为 gate 翻转而出现“写库成功但推送未提交”
- [ ] `Shutdown()` 在摘实例前完成 route/push/unregister 排空
- [ ] planned/manual drain 时实例先退出 `ready`，但在 grace window 内仍可承接旧连接的远端 push
- [ ] `RouteSubscribe` 故障时实例立刻退出 `routeable` 并开始 drain
- [ ] 实例摘除时不再承载本地连接

### 一致性验收

- [ ] `register` 不会因队列满形成永久漏路由
- [ ] `unregister` 队列满不阻塞主流程
- [ ] 路由缺失可被 reconcile 修复
- [ ] 滚动升级/实例重启后不会复用旧 `instance_id`
- [ ] legacy online key 不再参与 correctness 决策

### 性能验收

- [ ] 单聊跨实例 route-only 延迟 < 100ms
- [ ] 100 用户在线状态批量查询 < 10ms
- [ ] 慢连接场景允许少量 push drop，但连接不因单次写满被主动断开

---

## 18. 二期优化方向

以下能力不纳入本期，只作为二期候选：

1. broadcast 通道
2. hybrid selector
3. 广播分片与 dedup
4. 更细粒度 capability 控制面
5. 基于在线规模缓存的 route/broadcast 成本优化
6. 用路由表完全替代 legacy online key

二期开始前必须满足两个前提：

- 一期 lifecycle + route-only + presence 已稳定上线
- 当前关闭、路由收敛、在线状态语义已通过回归与灰度验证
