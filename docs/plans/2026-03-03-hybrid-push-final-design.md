# 多实例混合推送最终方案

**文档版本:** 1.5.7  
**创建日期:** 2026-03-03  
**修订日期:** 2026-03-06  
**状态:** 评审修订版

## 修订记录

| 版本 | 日期 | 变更摘要 |
|------|------|---------|
| 1.0 | 2026-03-03 | 初始方案 |
| 1.1 | 2026-03-03 | 修复连接级差集、协议轻量化、RouteStore 下沉到 WsServer、补充 TTL 续期与批量清理 |
| 1.2 | 2026-03-03 | 增加内部 ctx/cancel、在线状态聚合、配置默认值、辅助函数与类型命名修正 |
| 1.3 | 2026-03-04 | 修复广播自投递、远端信封异步处理、Shutdown 顺序初步修正、补充风险闸门 |
| 1.4 | 2026-03-05 | 增加 `instance_alive` 过滤、广播分片、背压治理、在线状态批量化、RouteConn DTO |
| 1.4.1 | 2026-03-05 | 增加两阶段门禁、分片预算修正、背压可观测性、惰性清理限流、分步迁移说明 |
| 1.4.2 | 2026-03-05 | 引入 `crossInstanceHealthy`、大群粗判广播、register/unregister 有界队列、协议进一步解耦 |
| 1.5.0 | 2026-03-06 | 基于架构 review 重写方案：修正关闭语义，明确“先停发送再排空再摘实例”；Route 写入改为“注册强保证、注销弱保证 + 周期校准”；补齐显式状态机与 PushBus 结构；收敛降级边界，移除高风险的无路由查询粗判广播 |
| 1.5.1 | 2026-03-06 | 将 HTTP `/msg/send` 纳入 `sendClosed` 门禁；移除本期 `ExcludeConnId` 实现与验收；补充慢连接“允许少量丢 push 但保持连接”策略；补充 `registerChan/unregisterChan` 收敛要求与关闭期公共错误码约定 |
| 1.5.2 | 2026-03-06 | 根据架构 review 收敛能力态与门禁分层：拆分 `Publish / Subscribe / RouteRead` capability，引入 `LifecycleGate`；明确大群上游 fanout 仍是本期边界；补齐 authoritative reconcile、心跳/校准解耦、在线状态 partial 语义与 dedup 回收 |
| 1.5.3 | 2026-03-06 | 明确 `instance_id` 为进程唯一标识，重启/升级不得复用；引入 `alive/ready/draining` 实例可路由模型；补齐 `/ready` 契约、RouteStore 的 `ready` 过滤，以及 `Subscribe=Degraded` 时 `unready + close ingress/send + drain` 的完整时序 |
| 1.5.4 | 2026-03-06 | 继续收敛能力态与接口契约：拆分 `PushRouteRead / PresenceRead`，修正跨实例发布门禁只依赖 `Publish`；补齐 `LifecycleGate` 的 `MarkUnready/Snapshot` 定义；将在线状态降级语义收敛到 `response.meta`；明确 `Subscribe=Degraded` 的批量 kick/drain 策略 |
| 1.5.5 | 2026-03-06 | 统一升级与故障处置为 `drain-exit`：新增 `DrainReason`，明确 `planned_upgrade / subscribe_fault / manual` 共用一套排空退出流程；移除 `Subscribe` 的 in-place recover 语义；将 drain 超时配置泛化为通用配置 |
| 1.5.6 | 2026-03-06 | 根据当前系统架构再次修订：拆分 `ready` 与 `routeable`，补充实例拥有索引以支撑 authoritative reconcile；将 `Subscribe` 拆为 `RouteSubscribe / BroadcastSubscribe`；为发送关闭补齐 `SendLease` 双阶段排空语义，并修正 drain 窗口的路由与 kick 假设 |
| 1.5.7 | 2026-03-06 | 继续收敛语义边界：明确“在线 = 有 socket 连接”，拆分 PushRoute 与 Presence 读路径；移除 `PushRouteRead=Degraded` 时的单聊广播兜底；修正 route stale cleanup 文案与 presence 聚合规则 |

---

## 1. 概述

### 1.1 背景

当前 nexo_v2 的实时推送完全依赖单实例 `WsServer` 本地内存连接表：

- `MessageService.SendMessage()` 仅通过 `MessagePusher.AsyncPushToUsers(...)` 触发推送
- `WsServer.processPushTask()` 只查询本地 `UserMap`
- `UserMap` 当前同时承担“本地连接管理”和“Redis 在线标记”两类职责

这套模型在单实例下可用，但在多实例部署下，跨实例在线用户无法实时收到消息。

### 1.2 本期目标

在**不修改消息写库主链路**、**不改变 `MessageService` 接口**的前提下，为 gateway 增加跨实例推送能力：

- 单聊优先精准路由
- 小群优先精准路由
- 大群按阈值切换到广播
- 跨实例组件故障时，不扩大影响面，本地连接推送保持可用，远端通过 pull 补偿

### 1.3 约束与边界

- 当前仓库的 seq 分配仍依赖 Redis；Redis 启动不可用时服务仍会启动失败，本期不处理
- 本期不改 WebSocket 对外协议结构，不改 `MessagePusher` 接口
- 本期只增强 gateway 推送路径，不改 MySQL/消息表结构
- 群聊成员枚举仍由 `MessageService -> GroupRepo` 全量查询，本期不解决大群上游 fanout 成本，只优化 gateway 跨实例分发成本与故障隔离
- 可靠性语义是“消息先入库，再尽力实时推送，最终由 seq + pull 保底”，不是“总线级严格送达”
- 优雅关闭的目标是**收敛丢失窗口**，不是承诺“关闭期间零损失”

---

## 2. 当前系统约束

方案必须服从当前代码边界，而不是重塑整个系统：

- `MessageService` 只依赖 `MessagePusher`，见 `internal/service/message_service.go`
- `WsServer` 负责连接接入、推送分发、协议转换，见 `internal/gateway/ws_server.go`
- `Client` 拥有独立 `readLoop` 生命周期，不受 `WsServer` 传入 ctx 直接控制，见 `internal/gateway/client.go`
- `UserMap` 是本地连接事实源，当前 Redis online key 只是分布式辅助标记，见 `internal/gateway/user_map.go`

因此，本方案的原则是：

1. 不把跨实例路由逻辑上推到 `MessageService`
2. 不让 `UserMap` 继续膨胀成“本地连接 + 分布式路由 + 在线状态”的 God Object
3. 不引入新的持久化基础设施，仍基于 Redis Pub/Sub + Redis Hash
4. 生命周期门禁通过独立 `LifecycleGate` 暴露给 HTTP/WS 入口，而不是让 handler 直接依赖 `WsServer` 内部状态

---

## 3. 备选方案与结论

### 方案 A：全量广播

所有跨实例消息都发到广播 channel，各实例按本地 `UserMap` 过滤。

优点：

- 最简单
- 无需路由表
- 容错路径直观

缺点：

- 单聊和小群成本过高
- 实例数和在线用户增长后网络放大明显
- 广播自投递和去重成本高

### 方案 B：全量精准路由

所有消息都先查用户路由，再发到目标实例。

优点：

- 带宽最省
- 对单聊最优

缺点：

- 大群场景 Redis 路由查询成本高
- 对路由一致性要求高
- 降级路径更脆弱

### 方案 C：混合路由（推荐）

单聊和小群走精准路由，大群走广播；本地连接始终优先本地投递。

优点：

- 保留单聊/小群的精准能力
- 大群可控
- 与当前 `WsServer` 架构兼容

缺点：

- 组件增多，生命周期管理更复杂
- 需要明确 route/broadcast 切换和降级边界

**最终选择：方案 C。**  
但采用保守版混合方案：**先保证一致性和关闭语义，再做成本优化**。原方案中的“大群粗判直接 broadcast（跳过路由查询）”风险较高，本期移出主路径，作为后续优化项。

---

## 4. 核心设计决策

| 决策项 | 选择 | 说明 |
|--------|------|------|
| 系统边界 | 改动集中在 gateway | 保持 `MessageService`、WS 对外协议、消息写库链路不变 |
| 路由粒度 | 连接级 | 使用 `UserId + ConnId + InstanceId + PlatformId`，避免同用户多实例时漏推 |
| 本地事实源 | `UserMap` | `UserMap` 只管理本地连接内存态 |
| 实例标识 | 进程唯一 `instance_id` | 每次进程启动自动生成；部署升级、实例重启后不得复用旧值 |
| 分布式路由 | `RouteStore + InstanceRouteIndex` | 用户维度 hash 负责读路径，实例维度拥有索引负责 authoritative reconcile 与 stale route 回收 |
| 实例可路由性 | `alive + ready + routeable + draining` | `ready` 只表示可接新 HTTP/WS 流量；`routeable` 才表示可被其他实例作为实时 push 目标 |
| 注册可靠性 | 强保证且不阻塞 eventLoop | `register` 不能永久丢失，但同步兜底不能直接阻塞 `WsServer` 的注册/注销主循环 |
| 注销可靠性 | 弱保证 + 索引兜底 | `unregister` 可退化为 `routeable` 过滤 + TTL + dual-index cleanup 收敛 |
| 路由修复 | TTL 续期 + dual-index authoritative reconcile | 既回补漏写，也能删除“用户已不在本实例，但用户 hash 仍被其他实例续期”的 stale field |
| 推送协议 | Envelope 元数据 + Payload 消息体 | 信封只放路由元信息，业务消息只保留一份 |
| 大群选择 | 查路由后再决定 route/broadcast | 先 correctness；本期仅优化 gateway 分发，不承诺消除上游成员枚举成本 |
| 在线语义 | `online = socket connected` | presence 表示用户当前有活跃 socket 连接，不等价于“仍可被跨实例实时路由命中” |
| 自回显处理 | 客户端合并/去重 | 本期不实现 `ExcludeConnId`，发送连接若收到自己的 push，由客户端按 `client_msg_id` 或 `server_msg_id/seq` 合并 |
| 慢连接策略 | 允许少量丢 push，不主动断链 | 写缓冲满时记录 drop，由客户端通过 seq/pull 补偿 |
| 降级原则 | 不扩大影响面 | 路由或总线异常时优先 local only + pull 补偿，不在未知目标实例集合时退化到广播 |
| 发送门禁 | `LifecycleGate + SendLease` | HTTP/WS 共用生命周期门禁；先停止新发送，再等待已准入请求完成，避免“已写库但因 gate 翻转而静默不推” |
| 关闭语义 | 先停 ingress，再停新 send，迁完连接后再摘路由与实例 | 避免 drain 窗口内仍在线的旧连接从路由层提前消失 |
| 升级/故障处置 | 按 reason 分层 `drain-exit` | `planned/manual` 先 `unready` 但短暂保留 `routeable`；`route_subscribe_fault` 立即 `unrouteable` |
| 状态机 | 能力态拆分 + 聚合观测态 | 拆分 `Publish / RouteSubscribe / BroadcastSubscribe / PushRouteRead / PresenceRead`；聚合态仅用于监控/告警 |

---

## 5. 架构设计

### 5.1 总体结构

```text
MessageService.SendMessage()
    -> MessagePusher.AsyncPushToUsers(msg, userIds, excludeConnId)
        -> WsServer.AsyncPushToUsers(...)
            -> pushChan -> pushLoop -> processPushTask()
                1. pushToLocalClients()
                2. if cross instance publishable:
                       dispatchCrossInstance()

跨实例基础设施：
    WsServer
      |- UserMap                本地连接事实源
      |- LifecycleGate          ingress/routeable/send-drain 统一门禁
      |- RouteStore             用户路由索引 + 实例拥有索引
      |- InstanceManager        instance_alive 注册/心跳/摘除
      |- PushBus                Redis Pub/Sub 发布/订阅
      |- HybridSelector         route/broadcast 选择
      |- OnlineStateWriter      兼容 online key 写入
      |- CrossInstanceCaps      publish/route-subscribe/broadcast-subscribe/push-route-read/presence-read 能力态
```

### 5.2 组件职责

#### WsServer

- 作为推送路径编排者
- 管理本地推送、跨实例分发、远端信封消费、优雅关闭
- 通过 `LifecycleGate` 接入门禁与发送门禁，不把门禁状态散落到 handler
- 不持久化路由，只编排 `UserMap` / `RouteStore` / `PushBus`

#### UserMap

- 仅维护本地连接
- 提供 `GetAll`、`HasConnection`、`SnapshotRouteConns`
- 不直接执行 Redis online / route I/O

#### RouteStore

- 保存用户维度路由 `nexo:route:user:{userId}` 与实例维度拥有索引 `nexo:route:inst:{instanceId}`
- 支持 register/unregister/query/ttl refresh
- 提供两类读路径：
  - PushRouteRead：结合 `alive=1 && routeable=1` 过滤可路由实例
  - PresenceRead：结合 `alive=1` 聚合“仍有 socket 连接”的远端实例，不受 `routeable` 影响
- 提供惰性清理和异步补偿清理
- 提供 authoritative reconcile 能力，既修复漏写，也删除“用户已完全离开本实例，但其 stale conn field 仍挂在用户 hash 中”的脏数据

#### InstanceManager

- 管理 `nexo:instance:alive:{instanceId}`
- 心跳只负责刷新实例活跃状态，本身必须轻量
- 负责把 `LifecycleGate` 的 `ready / routeable / draining` 快照，以及 `BroadcastSubscribe` 对应的 `broadcast_ready` 位镜像到 Redis
- 供 LB 判断实例是否仍可接流量，供 `RouteStore` 判断实例是否仍可被路由，供 sender 在 broadcast 选择前判断目标实例是否都仍具备广播接收能力
- 不在心跳回调里执行全量 reconcile；TTL 续期与校准走独立维护 loop
- 只在实例真正不再承载客户端时才摘除

#### PushBus

- 负责实例 route 通道和广播通道的发布/订阅
- 运行时错误回调给 `WsServer`
- `Close()` 后不再允许 publish

#### HybridSelector

- 基于远端在线目标数和涉及实例数选择 route 或 broadcast
- 不负责 Redis 查询
- 不直接处理 capability 降级；`WsServer` 根据能力态决定是否接受 selector 给出的 broadcast 偏好

#### OnlineStateWriter

- 兼容维护旧的 `nexo:online:{userId}` key
- 只用于历史调用方，如 `UserMap.IsOnline()`
- 不是跨实例推送的真相源

#### LifecycleGate

- 统一管理 `ready / routeable / ingressClosed / sendDraining / sendClosed / draining`
- 通过接口暴露给 WS 握手、WS `SendMessage`、HTTP `/msg/send` 和 `AsyncPushToUsers`
- 通过 `SendLease` 机制保证“已准入发送请求”可以在关闭期内走完整个写库 + 推送提交流程
- 对外提供统一的 shutdown 期错误语义与 HTTP status 映射入口
- 维护本地 `ready / routeable / ingressClosed / sendDraining / sendClosed / draining` 真相源，并驱动 `/ready` 与实例 `ready/routeable` 位同步变化

---

## 6. 结构定义

### 6.1 路由结构

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

- `nexo:route:user:{user_id}` 负责按目标用户高效读取远端连接
- `nexo:route:inst:{instance_id}` 负责表达“当前实例声称仍拥有的连接集合”，用于 authoritative reconcile
- `register / unregister / ttl refresh` 必须同时维护两套索引，使用同一 pipeline/multi 保证同轮写入尽量一致
- value 保持固定格式字符串，避免 JSON 解析开销；field 已经是 `conn_id`，不再在 value 中重复一份
- TTL 只负责兜底，不替代显式注销
- `instance_id` 必须是**进程唯一**：每次 pod/进程启动生成新值，重启与滚动升级后不得复用旧值
- 因为 `instance_id` 不复用，所以旧进程残留 route 会先被 `alive/routeable` 过滤，再由 TTL 与 reconcile 收敛
- `ReconcileInstanceRoutes()` 不能只看“当前还在线的 user hash”；必须结合 `nexo:route:inst:{instance_id}` 做权威 diff，才能删除“用户已完全离开本实例，但用户 hash 仍被其他实例续期”的 stale field

### 6.2 实例活跃结构

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
  "broadcast_ready": "1",
  "draining": "0",
  "started_at": "1709366400000",
  "last_heartbeat": "1709366430000",
  "conn_count": "1234"
}
```

字段语义：

- `alive` 由 key 是否存在表达：存在即进程仍存活
- `ready=1` 表示该实例可接新 HTTP/WS 流量，供 LB 与 `/ready` 使用
- `routeable=1` 表示该实例仍可被其他实例作为实时 push 目标
- `broadcast_ready=1` 表示该实例当前仍能接收广播 envelope；sender 只有在**所有目标实例**都满足此条件时才允许选择 broadcast
- `draining=1` 表示该实例正在迁移连接和排空任务
- `ready=0 && routeable=1` 是**正常升级/人工 drain** 的合法中间态：实例不再接新流量，但仍短暂承接旧连接的实时 push
- `ready=0 && routeable=0` 才表示该实例已经从实时路由层摘除；`subscribe_fault` 应立即进入该状态
- `BroadcastSubscribe=Degraded` 时应写入 `broadcast_ready=0`，但可以继续保持 `routeable=1`
- `routeable=0` 不代表“用户离线”；presence 仍应基于 `alive=1 + 连接尚未注销` 判断在线
- `ready=0` 不等于“实例已死”；它可能仍处于排空期，需要保留 `alive`

### 6.3 能力态

```go
type CapabilityState int32

const (
    CapabilityDisabled CapabilityState = iota
    CapabilityReady
    CapabilityDegraded
)

type CrossInstanceCapabilities struct {
    Publish            atomic.Int32
    RouteSubscribe     atomic.Int32
    BroadcastSubscribe atomic.Int32
    PushRouteRead      atomic.Int32
    PresenceRead       atomic.Int32
}

func (c *CrossInstanceCapabilities) CanPublishOutbound() bool {
    return c.Publish.Load() == int32(CapabilityReady)
}

func (c *CrossInstanceCapabilities) CanReceiveRouteEnvelope() bool {
    return c.RouteSubscribe.Load() == int32(CapabilityReady)
}

func (c *CrossInstanceCapabilities) CanReceiveBroadcastEnvelope() bool {
    return c.BroadcastSubscribe.Load() == int32(CapabilityReady)
}

func (c *CrossInstanceCapabilities) CanReadPushRoute() bool {
    return c.PushRouteRead.Load() == int32(CapabilityReady)
}

func (c *CrossInstanceCapabilities) CanReadPresence() bool {
    return c.PresenceRead.Load() == int32(CapabilityReady)
}
```

要求：

- `Publish` 只控制是否允许向远端发布 envelope
- `RouteSubscribe` 只控制实例 route 通道订阅；这是“本实例还能否接收定向远端 push”的关键能力
- `BroadcastSubscribe` 只控制广播通道订阅；故障时应优先降级为 route-only，而不是直接摘整实例
- `PushRouteRead` 只控制实时推送链路上的路由查询
- `PresenceRead` 只控制在线状态查询
- 聚合态 `Disabled / Healthy / Degraded` 仅用于监控展示，不得直接作为 `processPushTask()` 的唯一门禁
- 状态流转必须集中管理，不允许散落在 publish/subscribe 各处自行写状态

### 6.4 生命周期门禁

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

- `LifecycleGate` 是本地 `ready / routeable / draining` 的唯一真相源；`InstanceManager` 只做镜像写入
- `DrainReason` 只用于表达 drain 的触发原因、观测和审计；当前三类 reason 都走同一套 `drain-exit` 流程
- 本期 `LifecycleGate` 不提供 reopen / mark-ready 语义；进入 drain 的当前进程必须以退出为终态
- HTTP handler 依赖 `LifecycleGate` 接口，而不是直接依赖 `WsServer`
- WS 握手、WS `WSSendMsg`、HTTP `/msg/send` 与 `AsyncPushToUsers()` 必须共用同一门禁来源
- WS/HTTP 发送入口在调用 `MessageService.SendMessage()` 前必须获取 `SendLease`；`BeginSendDrain()` 之后拒绝新 lease，但允许已获取 lease 的请求走完整个写库 + 推送提交流程
- `CloseSendPath()` 只能在 in-flight send lease 排空后执行；它是“硬关闭”，而不是“开始关闭”
- 关闭期统一错误语义与 HTTP status 映射从这里发散，不在单个入口各自 hardcode

### 6.5 推送协议

```go
type PushMode string

const (
    PushModeRoute     PushMode = "route"
    PushModeBroadcast PushMode = "broadcast"
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
    ChunkPushId    string               `json:"chunk_push_id,omitempty"`
    ChunkIndex     int                  `json:"chunk_index,omitempty"`
    ChunkTotal     int                  `json:"chunk_total,omitempty"`
    Mode           PushMode             `json:"mode"`
    TargetUserIds  []string             `json:"target_user_ids,omitempty"`
    TargetConnMap  map[string][]ConnRef `json:"target_conn_map,omitempty"`
    SourceInstance string               `json:"source_instance"`
    SentAt         int64                `json:"sent_at"`
    Payload        *PushPayload         `json:"payload"`
}
```

修正点：

- `MsgId / ConversationId / SessionType` 不再在 envelope 顶层重复存一份
- envelope 只承载“如何投”，payload 只承载“投什么”
- `PushContent` 使用独立命名结构，避免匿名结构在 `MessageData`/`PushPayload` 间继续扩散

### 6.6 PushBus 接口

```go
type PushBus interface {
    PublishToInstance(ctx context.Context, instanceId string, env *PushEnvelope) error
    PublishBroadcast(ctx context.Context, env *PushEnvelope) error
    SubscribeInstance(ctx context.Context, instanceId string, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) error
    SubscribeBroadcast(ctx context.Context, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) error
    Close() error
}

type RedisPushBus struct {
    rdb      *redis.Client
    instSub  *redis.PubSub
    broadSub *redis.PubSub
    closed   atomic.Bool
}
```

实现约束：

- 实例通道和广播通道使用两个独立 `PubSub` handle
- `Close()` 必须同时关闭两个订阅句柄
- 运行时订阅错误通过 `onRuntimeError` 回传给 capability 控制器，不只打日志

---

## 7. 核心流程

### 7.1 `processPushTask`

```go
func (s *WsServer) processPushTask(ctx context.Context, task *PushTask) {
    payload := s.toPushPayload(task.Msg)
    msgData := s.toMessageData(payload)

    s.pushToLocalClients(ctx, task, msgData)

    if !s.crossCaps.CanPublishOutbound() || s.pushBus == nil {
        return
    }

    s.dispatchCrossInstance(ctx, task, payload)
}
```

规则：

- 本地推送始终优先
- 只有 `Publish` ready 时才允许尝试跨实例分发
- 是否可读推送路由、是否退化为 local only + pull 补偿，在 `dispatchCrossInstance()` 内部决策
- `RouteSubscribe` 降级会触发本实例 `unready + unrouteable + close ingress/send + drain`；已进入系统的 in-flight outbound publish 可继续排空
- `payload` / `msgData` 在边界层各转换一次，不重复构造

### 7.2 本地推送

保持现有语义：

- 遍历 `task.TargetIds`
- 查本地 `UserMap.GetAll(userId)`
- 调用 `client.PushMessage(...)`

这里不引入跨实例逻辑，避免本地热路径复杂化。

### 7.3 跨实例分发

```go
func (s *WsServer) dispatchCrossInstance(ctx context.Context, task *PushTask, payload *PushPayload) {
    if s.routeStore == nil || !s.crossCaps.CanReadPushRoute() {
        s.handlePushRouteUnavailable(ctx, task, payload, nil)
        return
    }

    routeMap, err := s.routeStore.GetUsersConnRefs(ctx, task.TargetIds)
    if err != nil {
        s.handlePushRouteUnavailable(ctx, task, payload, err)
        return
    }

    remoteRouteMap := filterOutLocalInstance(routeMap, s.instanceId)
    if len(remoteRouteMap) == 0 {
        return
    }

    onlineTargetUsers := uniqueUsers(remoteRouteMap)
    instanceCount := uniqueInstances(remoteRouteMap)
    mode := s.selector.Select(task.Msg.SessionType, len(onlineTargetUsers), len(instanceCount))
    if mode == PushModeBroadcast && !s.instanceManager.AllBroadcastReady(instanceCount) {
        mode = PushModeRoute
    }

    switch mode {
    case PushModeRoute:
        s.publishRouteEnvelopes(ctx, task, payload, remoteRouteMap)
    case PushModeBroadcast:
        s.publishBroadcastEnvelopes(ctx, task, payload, onlineTargetUsers)
    }
}
```

修正点：

- 不再使用“群成员数达到阈值就跳过路由查询直接 broadcast”的粗判逻辑
- route/broadcast 决策基于**真实在线路由结果**
- 只有目标实例全部 `broadcast_ready=1` 时才允许选择 broadcast；否则强制回退为 route
- broadcast 的 `TargetUserIds` 只包含在线远端用户，不扩散离线成员

### 7.4 推送路由不可用时的降级

推送路由不可用时不再一刀切广播。

规则：

1. 单聊：
   - 默认退化为 local only + pull 补偿
   - 不因路由读取失败改走广播；sender 在不知道目标实例集合时不能证明 broadcast 是安全的
2. 群聊：
   - 默认退化为 local only + pull 补偿
   - 不因 Redis 路由查询失败把大群广播扩散到所有实例
3. 若总线也不健康：
   - 统一 local only
4. 在线状态查询失败不应写坏推送 capability：
   - `PresenceRead` 只影响 `/user/get_users_online_status`
   - 不得因此阻断消息推送主链路

这样做的原因：

- 原方案“路由查询失败 -> 广播兜底”会在 sender 不知道目标实例集合时放大影响面
- `PresenceRead` 和 `PushRouteRead` 如果共用一个开关，非关键接口会把实时推送一起拖下水
- 本期目标是“不扩大影响面”，不是“任何失败都宁可多发”

### 7.5 route 模式

```go
func (s *WsServer) publishRouteEnvelopes(ctx context.Context, task *PushTask, payload *PushPayload, routeMap map[string][]RouteConn) {
    grouped := groupByInstance(routeMap)
    for instId, refs := range grouped {
        env := &PushEnvelope{
            PushId:         uuid.New().String(),
            Mode:           PushModeRoute,
            TargetConnMap:  map[string][]ConnRef{instId: refs},
            SourceInstance: s.instanceId,
            SentAt:         time.Now().UnixMilli(),
            Payload:        payload,
        }
        if err := s.pushBus.PublishToInstance(ctx, instId, env); err != nil {
            s.onPublishFailure(err)
        } else {
            s.onPublishSuccess()
        }
    }
}
```

要求：

- 每个实例一个独立 envelope
- 接收端零查询，直接根据 `TargetConnMap[self]` 投递
- publish 成功/失败必须反馈 capability 控制器

### 7.6 broadcast 模式

```go
func (s *WsServer) publishBroadcastEnvelopes(ctx context.Context, task *PushTask, payload *PushPayload, targetUsers []string) {
    chunkPushId := uuid.New().String()
    chunks := splitTargetUserIdsByMarshalSize(targetUsers, payload, s.cfg.WebSocket.MaxBroadcastChunkBytes)
    for idx, chunk := range chunks {
        env := &PushEnvelope{
            PushId:         uuid.New().String(),
            ChunkPushId:    chunkPushId,
            ChunkIndex:     idx + 1,
            ChunkTotal:     len(chunks),
            Mode:           PushModeBroadcast,
            TargetUserIds:  chunk,
            SourceInstance: s.instanceId,
            SentAt:         time.Now().UnixMilli(),
            Payload:        payload,
        }
        if err := s.pushBus.PublishBroadcast(ctx, env); err != nil {
            s.onPublishFailure(err)
        } else {
            s.onPublishSuccess()
        }
    }
}
```

分片要求：

- 以真实 `json.Marshal(envelope)` 大小为准
- 使用增量预算 + 临界点精确校验，避免每追加一个 userId 都全量 marshal
- 单片上限默认 128KB

### 7.7 远端 envelope 处理

```go
func (s *WsServer) onRemoteEnvelope(ctx context.Context, env *PushEnvelope) {
    if env.Mode == PushModeBroadcast && env.SourceInstance == s.instanceId {
        return
    }

    if _, loaded := s.pushDedup.LoadOrStore(env.PushId, time.Now().UnixMilli()); loaded {
        return
    }

    msgData := s.toMessageData(env.Payload)

    switch env.Mode {
    case PushModeRoute:
        deliverRouteEnvelopeLocally(msgData, env.TargetConnMap[s.instanceId])
    case PushModeBroadcast:
        deliverBroadcastEnvelopeLocally(msgData, env.TargetUserIds)
    }
}
```

要求：

- 广播必须有 `SourceInstance` 自投递防护
- route 模式不依赖 `SourceInstance`
- `remoteEnvChan` 只做异步解耦，不改变业务语义
- `pushDedup` 必须有 TTL 或容量上限回收；不能无限增长

### 7.8 自回显与慢连接策略

#### 自回显

本期**不实现** `ExcludeConnId`。

含义：

- 同一发送连接可能会再次收到自己刚发送消息的实时 push
- 若客户端已基于 `client_msg_id` 或 `server_msg_id/seq` 做消息合并，这不会形成重复展示
- `/msg/send` 作为后端服务常用入口，本身也不存在“当前发送连接”概念，因此不值得为该能力改造 `MessageService` 入参链路

#### 慢连接

本期慢连接策略是：**允许少量丢实时 push，但保持连接**。

要求：

- `client_conn.go` 必须真正接入现有 `write_wait / pong_wait / ping_period / write_channel_size` 配置
- 当连接写缓冲满时，`PushMessage` 返回 `ErrWriteChannelFull`
- 服务端记录指标和日志，但**不因单次或少量写满主动断开连接**
- 客户端通过 `GetNewestSeq` / `PullMsg` 对这类 drop 做补偿
- `KickOnline` 属于 drain 控制路径，不应复用“普通 push 入异步写队列后立刻关闭连接”的语义；必须使用“控制消息优先 + 短超时 flush 后 close”或等价机制

这意味着：

- “连接在线”不等于“实时 push 必定送达”
- 实时链路是尽力而为，最终一致性仍由 seq + pull 保障

---

## 8. 路由写入与一致性设计

### 8.1 原则

路由写入不能再简单地“一律入有界队列，满了就丢”。

因为：

- `register` 丢失会导致连接整个生命周期都不在跨实例路由里
- 心跳续期只会续已有 key 的 TTL，补不回漏掉的 field
- 这类错误不是短暂背压，而是永久路由缺失

### 8.2 新的写入语义

#### 注册：强保证，但不能阻塞 `eventLoop`

流程：

1. `UserMap.Register()` 先更新本地事实源
2. 尝试将 `register` 事件写入 `routeWriteChan`
3. 若队列满：
   - 将连接写入 `mustRegisterSet` / `pendingRegisterMap`
   - 唤醒独立 `routeRepairWorker` 以短超时 + 重试方式补写 Redis
4. `routeRepairWorker` 成功后清理 pending 项；失败则保留到下一轮 retry/reconcile

结论：

- `register` 不能直接 drop
- 强保证应由独立 route writer / repair worker 提供，**不能**在 `WsServer.eventLoop` 或 WS 握手线程里直接执行阻塞式 Redis I/O
- 失败时必须有后续 retry 或 reconcile 兜底

#### 注销：弱保证

流程：

1. `UserMap.Unregister()` 先更新本地事实源
2. 尝试异步写 `RouteStore.UnregisterConn()`，同时删除用户 hash 与实例拥有索引
3. 若队列满：
   - 允许只记日志和指标
   - 依赖 `alive/routeable` 路由过滤、TTL 过期和 dual-index cleanup 收敛脏数据

结论：

- `unregister` 可以 best-effort
- 这类误差是“多一条 stale route”，不是“永久漏推”

### 8.3 周期校准

仅靠 register/unregister 事件还不够，需要可恢复能力。

新增：

```go
func (m *UserMap) SnapshotRouteConns() []RouteConn
func (s *RouteStore) ReconcileInstanceRoutes(ctx context.Context, instanceId string, conns []RouteConn) error
```

校准策略：

- `InstanceManager` 心跳每 10s 只刷新 `instance_alive`
- `RouteTTLRefresher` 以独立 loop 定期批量刷新本实例涉及用户的 user hash TTL 与 instance index TTL
- 每隔 `route_reconcile_interval_seconds`（默认 60s）由独立 reconcile loop 对本实例活动连接做一次全量校准
- reconcile 必须读取 `nexo:route:inst:{instance_id}` 作为“该实例曾声明拥有的连接集合”，再与 `SnapshotRouteConns()` 做 authoritative diff：
  - 补齐当前连接缺失的 user hash / instance index
  - 删除 instance index 中存在但本地快照已不存在的 `(user_id, conn_id)`
  - 回收对应 user hash 上的 stale field，即使该 user hash 仍被其他实例续期
- reconcile loop 必须避免重入；上一次尚未结束时本轮跳过并记指标

作用：

- 修复 register retry 仍失败的连接
- 修复 Redis 重启、驱逐或人工清理导致的路由缺失
- 修复 event drop 带来的长期不一致
- 清理“用户仍在线但某个 stale conn field 因 user hash 被其他实例续期而长期残留”的脏数据

### 8.4 推送路由读取

读路径规则：

1. 批量查询 `GetUsersConnRefs(userIds...)`
2. 读取结果后只保留 `alive=1 && routeable=1` 的实例作为可路由目标
3. 每次最多惰性清理 `route_stale_cleanup_limit` 条脏记录，必要时同时 `HDEL` user hash field 与 `SREM` instance index member
4. 超额部分放入异步 cleanup 队列

这个顺序不可颠倒：

- 先过滤，再惰性删除
- 过滤是 correctness，删除只是优化
- `ready=0 && routeable=1` 的实例仍处于**正常 drain 中间态**，其 route 仍然有效，不参与 stale cleanup
- 只有 owner 实例 `alive=0`、`routeable=0` 且该连接已不在 instance index 中，或明确判定为残留 field，才允许进入 stale cleanup

### 8.5 Presence 读取

presence 读路径与 push route 读路径必须分开。

规则：

1. 批量查询 `GetUsersPresenceConnRefs(userIds...)`
2. 读取结果后只保留 `alive=1` 的实例作为远端 presence 候选
3. `routeable=0`、`broadcast_ready=0`、`draining=1` 都**不应**直接把该连接判为离线
4. 只有当连接显式注销、实例 `alive=0` 且 TTL 收敛、或明确判定为 stale field 时，才能从 presence 结果中移除

原因：

- presence 语义是“用户仍有 socket 连接”，而不是“此连接仍可被跨实例实时路由命中”
- `RouteSubscribe=Degraded` 进入 fault-drain 后，到连接真正被 `KickOnline/Close + unregister` 收敛前，用户仍应显示为在线

---

## 9. 在线状态设计

### 9.1 双源模型

本期仍保留双源：

- `nexo:online:{userId}`：兼容历史在线判断
- `nexo:route:user:{userId}`：跨实例推送和在线状态聚合的真实数据来源

### 9.2 责任边界

- `UserMap` 不再直接写 Redis online key
- 由 `OnlineStateWriter` 异步维护 online key
- `GetUsersOnlineStatus` 走“本地连接 + RouteStore presence 批量查询”

### 9.3 `GetUsersOnlineStatus`

规则：

- 单次请求限制 `user_ids <= 100`
- 远端 presence 查询使用单次 batch/pipeline
- 查询超时或失败时允许退化为本地结果，但必须显式返回 `meta.partial=true` 与 `meta.data_source=local_only`
- 远端 presence 聚合只过滤 `alive=1`，**不以** `routeable` 作为在线过滤条件
- 若远端实例处于 `draining=1` 或 `routeable=0`，但连接尚未注销，该用户仍视为在线；本期可不在响应体暴露 draining 明细

返回语义：

- `detail_platform_status` 中包含本地和远端连接
- 远端连接需排除本实例，避免重复
- 为保持 `data` 仍然是原有结果数组，降级元信息应放在统一响应外层的 `response.meta`
- 建议新增 `response.SuccessWithMeta(...)` 或等价 helper，返回形如：

```json
{
  "code": 0,
  "message": "success",
  "data": [ ...online status results... ],
  "meta": {
    "partial": true,
    "data_source": "local_only"
  }
}
```

- 不能把“远端未知”伪装成“远端离线”
- 不能把“实时不可路由”伪装成“socket 已离线”

---

## 10. 状态机与降级

### 10.1 能力态初始化与流转

初始状态：

- `cross_instance.enabled=false` -> `Publish / RouteSubscribe / BroadcastSubscribe / PushRouteRead / PresenceRead = Disabled`
- `cross_instance.enabled=true` -> 五类 capability 独立初始化，互不绑定
- 聚合态 `Healthy / Degraded / Disabled` 仅用于指标、日志和面板展示

进入 `Degraded` 的条件按能力拆分：

- `Publish`：连续 publish 失败次数达到阈值
- `RouteSubscribe`：实例 route 通道订阅建立失败，或出现运行时订阅错误
- `BroadcastSubscribe`：广播通道订阅建立失败，或出现运行时订阅错误
- `PushRouteRead`：推送路由查询达到失败阈值
- `PresenceRead`：在线状态批量查询达到失败阈值

恢复到 `Ready` 的条件也按能力拆分：

- `Publish`：连续 publish 成功清零失败计数
- `BroadcastSubscribe`：广播订阅恢复正常并通过探活
- `PushRouteRead`：推送路由读恢复正常并通过探活
- `PresenceRead`：在线状态读恢复正常并通过探活

特殊约束：

- `RouteSubscribe=Degraded` 后，当前进程必须进入 `fault-drain` 并最终退出；**不做 in-place recover**
- `BroadcastSubscribe=Degraded` 后，当前进程只退出 broadcast 能力，优先降级为 route-only；若同时 `PushRouteRead=Degraded`，则群聊退化为 local only + pull 补偿
- 新实例启动后通过新的 `instance_id` 和新的订阅句柄接管流量；旧实例只负责排空，不再回到 `Ready`

### 10.2 降级策略

| 场景 | 行为 |
|------|------|
| `cross_instance.enabled=false` | 纯本地推送 |
| `Publish=Degraded` | 停止 outbound 跨实例发布，仅保留本地推送 |
| `RouteSubscribe=Degraded` | 进入 `fault-drain`：立即 `ready=0, routeable=0`，触发 `CloseIngress/BeginSendDrain/EnterDraining`，等待 in-flight send lease 收敛后 `CloseSendPath()`，并开始客户端迁移；排空完成后退出进程 |
| `BroadcastSubscribe=Degraded` | 本实例写入 `broadcast_ready=0`；sender 看到目标实例包含该标记时必须强制回退为 route，本实例自身也禁用 broadcast 选择；不直接触发实例 drain |
| `PushRouteRead=Degraded` | 推送路由能力降级；统一退化为 local only + pull 补偿，不再做广播兜底 |
| `PresenceRead=Degraded` | 在线状态接口退化为本地结果 + `response.meta.partial=true`；不影响消息推送主链路 |
| `remoteEnvChan` 满 | 限时阻塞，超时丢弃并记指标 |

### 10.2.1 capability 聚合态

仅用于观测：

- 任一 capability `Degraded` -> 聚合态显示 `Degraded`
- 五者全 `Ready` -> 聚合态显示 `Healthy`
- 全 `Disabled` -> 聚合态显示 `Disabled`

禁止行为：

- 不能用聚合态直接决定 `processPushTask()` 是否允许 outbound publish

### 10.3 可用性边界

本期承诺：

- 跨实例组件故障不影响消息已写入数据库
- 跨实例组件故障不影响本地连接推送
- `RouteSubscribe` 故障会让实例立刻退出可路由状态并开始 `fault-drain`，但已进入系统的 in-flight outbound push 仍可在排空期内完成
- `BroadcastSubscribe` 故障不会直接摘整实例；它会通过 `broadcast_ready=0` 让涉及该实例的广播发送统一回退为 route-only / local-only
- 正常升级与人工摘流量走 `planned/manual drain`：实例会先退出 `/ready`，但在短暂 routeable grace 窗口内继续承接旧连接的远端实时 push，直到连接迁移完成或达到上限
- `PushRouteRead` 故障不会扩大影响面到广播；统一依赖 local push + seq/pull 补偿
- presence 语义始终表示“用户仍有 socket 连接”，不因 `routeable=0` 直接变成离线
- 在线状态查询故障不应阻断实时推送主链路
- 客户端可通过 seq + pull 补偿远端未实时收到的消息

本期不承诺：

- Pub/Sub 级别严格送达
- 关闭期间完全零丢失
- Redis 故障下仍能维持写库/seq 分配

---

## 11. WsServer 生命周期与关闭流程

### 11.1 新增门禁与发送租约

建议新增独立 `LifecycleGate`，统一管理以下本地状态：

- `ready`：是否仍可接新 HTTP/WS 流量
- `routeable`：是否仍可被其他实例作为实时 push 目标
- `ingressClosed`：拒绝新 HTTP/WS 连接
- `sendDraining`：拒绝新的 `SendLease`
- `sendClosed`：硬拒绝新的 `WSSendMsg`、HTTP `/msg/send` 和 `AsyncPushToUsers`
- `draining`：进入排空阶段，不再接收新的 drain 外部工作

原因：

- `ready` 和 `routeable` 语义不同；正常 drain 必须允许 `ready=0` 但暂时 `routeable=1`
- 仅靠 handler 前置判定不够，已建立的 WS 连接仍可继续 `SendMessage`
- 如果没有 `SendLease`，会出现“请求在 gate 关闭前进入，写库成功后却因 `AsyncPushToUsers` 被拒绝而静默不推”的竞态

### 11.1.1 `/health` 与 `/ready`

建议区分：

- `/health`：只表示进程存活，映射 `alive`
- `/ready`：只表示实例是否还能接新流量，映射 `ready`

规则：

- `RouteStore` 的实时路由过滤必须看 `routeable`，**不能**看 `/ready`
- `RouteSubscribe=Degraded`、人工 drain、优雅关闭开始时，`/ready` 必须先失败
- `planned/manual drain` 期间允许 `ready=0 && routeable=1`
- `alive` 仍保持为真，直到排空完成并摘实例
- LB 只能基于 `/ready` 做新流量分配，不能继续使用 `/health`

### 11.1.2 Drain Reason 与迁移时序

本期统一三类 drain reason：

1. `planned_upgrade`
   - 正常升级、滚动发布、节点替换
2. `subscribe_fault`
   - `RouteSubscribe=Degraded`、实例 route 订阅句柄异常、实例 route 订阅运行时错误
3. `manual`
   - 人工摘流量、手动排空

三类 reason 共用同一套 `drain-exit` 时序：

#### `planned_upgrade / manual`

1. 立刻执行 `MarkUnready() + CloseIngress() + BeginSendDrain() + EnterDraining()`
2. 停止 ingress 后，等待 in-flight `SendLease` 收敛；收敛后执行 `CloseSendPath()`
3. 保持 `alive=1 + routeable=1` 与必要的 outbound publish 能力，给既有连接一个有限迁移窗口
4. 经过短暂 grace period 后，启动独立 kick worker，对现有连接执行分批 `KickOnline()`
5. 当本地连接事实源收敛为 0，或达到 `drain_routeable_grace_seconds` / `drain_timeout_seconds` 上限后，执行 `MarkUnrouteable()`
6. 继续排空 `pushChan / remoteEnvChan / routeWriteChan / unregisterChan` 后退出；后续流量由新实例接管

#### `subscribe_fault`

1. 立刻执行 `MarkUnready() + MarkUnrouteable() + CloseIngress() + BeginSendDrain() + EnterDraining()`
2. 等待 in-flight `SendLease` 收敛；收敛后执行 `CloseSendPath()`
3. 启动分批 `KickOnline()` / `Close()` 迁移现有连接
4. 排空剩余任务后退出；当前进程不等待订阅恢复

约束：

- `planned_upgrade` 与 `manual` 用于正常运维场景，目标是平滑迁移连接并退出旧实例
- `subscribe_fault` 用于 route 通道订阅异常场景；当前实例只排空，不等待订阅恢复后回到 serving
- kick 必须有节流，避免在 drain 时瞬时打爆重连风暴
- kick 不能只依赖“控制消息入普通异步写队列后立刻 close”这种不可靠实现；必须采用控制消息优先发送并在短超时后 close 的语义
- 推荐按 `drain_kick_batch_size` 分批，每批之间 sleep `drain_kick_interval_ms`
- 推荐设置 `drain_timeout_seconds` 作为最长迁移窗口
- drain 完成判断应基于**本地连接事实源**（如 `UserMap/onlineConnNum`），Redis `conn_count` 仅用于观测
- 该流程与优雅关闭共享同一套 `Kick/Close + unregister 收敛` 逻辑，而不是额外维护第二套状态机

### 11.2 发送关闭语义

发送关闭必须分两阶段，而不是一个 `sendClosed` 位一刀切：

1. 发送入口获取 `SendLease`
   - `HandleSendMsg` 必须在调用 `MessageService.SendMessage()` 前获取 `SendLease`
   - `MessageHandler.SendMessage` 必须通过注入的 `LifecycleGate` 在调用 `MessageService.SendMessage()` 前获取 `SendLease`
2. `BeginSendDrain()`
   - 拒绝新的 `SendLease`
   - 允许已经拿到 `SendLease` 的请求走完整个写库 + `AsyncPushToUsers()` 提交流程
3. `CloseSendPath()`
   - 只能在 in-flight `SendLease` 归零后执行
   - 执行后 `AsyncPushToUsers()` 才开始硬拒绝新入队

这样才能保证：

- 不再接纳新的发送请求
- 已经准入的发送请求不会出现“消息提交成功但推送入口已关”的不透明行为
- `sendClosed` 只表示“硬关闭已生效”，不是“开始关闭”

### 11.2.1 关闭期公共错误码

本期应新增统一错误语义，例如：

- `ErrServerShuttingDown`（业务错误码）
- HTTP 场景通过统一 response helper 映射为 `503 Service Unavailable`
- WS 场景写入 `WSResponse.ErrCode/ErrMsg`

避免出现：

- HTTP `/msg/send` 返回一种错误
- WS `WSSendMsg` 返回另一种错误
- 握手失败再写第三种字符串

要求：

- 不在单个 handler 里直接 hardcode `503`；统一由 `pkg/response` 或等价 helper 负责
- `MessageHandler` 只依赖 `LifecycleGate + MessageService`，不直接依赖 `WsServer`

### 11.3 修正后的关闭顺序

```text
1. MarkUnready()             `/ready` 立即失败，停止被 LB 分配新流量
2. CloseIngress()            拒绝新连接/新 HTTP 请求
3. BeginSendDrain()          拒绝新的 `SendLease`
4. 停止 Hertz ingress        不再接收外部新流量
5. 等待 inflight send lease  清空已准入但未完成的发送请求
6. CloseSendPath()           对外硬关闭新的 WSSendMsg / HTTP `/msg/send` / AsyncPushToUsers
7. planned/manual drain 时保持 `routeable=1`，subscribe_fault 时此时已 `routeable=0`
8. 保持实例 alive + PushBus 继续运行，排空 pushChan / remoteEnvChan
9. 主动 Kick/Close 现有 client，等待 unregister 与 routeWrite 收敛
10. 本地连接清零或达到 routeable grace 上限后，MarkUnrouteable()
11. 停止 InstanceManager      摘除 instance_alive
12. 关闭 PushBus              停止接收新的远端消息
13. cancel internal ctx       退出 eventLoop / pushLoop / remoteLoop / routeWriteLoop
14. WaitGroup 收敛            保证无 goroutine 残留
```

关键修正：

- **必须先 `ready=0`，再停 ingress**
- **planned/manual drain 不能在连接尚未迁走前就 `routeable=0`**
- **不能在排空前 stop `InstanceManager`**
- 否则实例要么继续接入新流量，要么还挂着本地连接却已经从路由层完全消失

### 11.4 排空对象

关闭时不能只看 `pushChan`，还必须等待：

- `sendLease + inflightSend`
- `pushChan + inflightPush`
- `remoteEnvChan + inflightRemoteEnv`
- `routeWriteChan + inflightRouteWrite`
- `unregisterChan + inflightUnregister`

否则会出现：

- 已准入发送请求尚未完成就被强行硬关闭
- 远端消息已接收但未投递即被 cancel
- route unregister/register 已入队但未写 Redis
- client 已被 Kick/Close，但本地连接计数和后续清理仍未收敛

### 11.4.1 本地连接事件队列

除 `routeWriteChan` 外，还需要明确现有 `registerChan / unregisterChan` 的收敛语义：

- `registerChan` 不允许在连接升级后无限阻塞；若注册入队超时，应主动关闭刚建立连接
- `unregisterChan` 不应继续保留“队列满就 silent drop”的语义，至少在 shutdown 期间必须保证收敛
- 关闭期的 `Kick/Close` 依赖 `unregister` 真正执行，否则 `onlineConnNum`、online key 和后续路由清理都会漂移

### 11.5 关闭目标

关闭期间的目标不是“所有连接都优雅自然退出”，而是：

- 新发送请求不再进入系统
- 已经准入的发送请求与本地/远端推送任务尽量排空
- 现有客户端被显式断开并迁移到其他实例
- 正常 drain 窗口内，既有连接在迁移完成前仍可接收远端实时 push
- 实例只在不再承载客户端后摘除

---

## 12. 配置设计

```yaml
websocket:
  push_channel_size: 10000
  push_worker_num: 10
  write_wait: 10s
  pong_wait: 30s
  ping_period: 27s
  write_channel_size: 256

  remote_env_channel_size: 1024
  remote_env_block_timeout_ms: 50
  max_broadcast_chunk_bytes: 131072
  push_dedup_ttl_seconds: 300

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
    broadcast_subscribe_fail_threshold: 3
    push_route_fail_threshold: 3
    presence_read_fail_threshold: 3
    recover_probe_interval_seconds: 5
    drain_timeout_seconds: 30
    drain_routeable_grace_seconds: 15
    drain_kick_batch_size: 200
    drain_kick_interval_ms: 100

  hybrid:
    enabled: true
    group_route_max_targets: 100
    group_route_max_instances: 4
    force_broadcast: false
```

说明：

- `cross_instance.enabled=false` 应保持默认，确保可回滚到单实例行为
- `instance_id` 为空时应在进程启动时自动生成；生产环境不建议手工配置稳定值
- 若必须显式配置 `instance_id`，也必须保证**进程唯一**，不能在重启或滚动升级后复用
- 移除 `fast_broadcast_group_min_targets`，避免不可靠优化进入主设计
- 保留 `force_broadcast` 用于灰度验证广播链路
- `heartbeat_second` 只控制 `instance_alive` 心跳；不再隐式绑定 route TTL refresh / reconcile
- `route_subscribe_fail_threshold` 默认为 1；route 通道订阅故障是实例级故障，应快速触发 `fault-drain`
- `broadcast_subscribe_fail_threshold` 可大于 1；广播链路优先按能力降级处理，而不是直接摘整实例
- `push_route_fail_threshold` 与 `presence_read_fail_threshold` 必须独立配置，避免在线状态查询把推送主链路一起拖下水
- `recover_probe_interval_seconds` 只用于 `Publish / BroadcastSubscribe / PushRouteRead / PresenceRead` 的恢复探活；**不用于 `RouteSubscribe` in-place recover**
- `drain_routeable_grace_seconds` 只用于 `planned/manual drain`；控制实例在退出 `/ready` 后继续保留 `routeable=1` 的最长窗口
- `drain_timeout_seconds / drain_kick_batch_size / drain_kick_interval_ms` 用于约束升级、订阅故障和人工 drain 的迁移风暴
- `push_dedup_ttl_seconds` 用于限制去重表内存增长
- `write_wait / pong_wait / ping_period / write_channel_size` 不再只是配置项，必须真正接入 `client_conn.go`

---

## 13. 改动范围

### 13.1 新增文件

- `internal/gateway/instance_manager.go`
- `internal/gateway/route_store.go`
- `internal/gateway/push_bus.go`
- `internal/gateway/push_protocol.go`
- `internal/gateway/push_selector.go`
- `internal/gateway/online_state_writer.go`
- `internal/gateway/lifecycle_gate.go`
- 对应单元测试文件

### 13.2 修改文件

- `internal/gateway/ws_server.go`
  - 增加跨实例组件编排、capability 状态、门禁、`SendLease`、队列与关闭流程
- `internal/gateway/client.go`
  - 将 `KickOnline` 收敛为 drain 控制路径，补齐 flush-before-close 或等价语义
- `internal/gateway/client_conn.go`
  - 接入 `write_wait / pong_wait / ping_period / write_channel_size` 配置
  - 明确写缓冲满时的慢连接语义
  - 区分普通业务 push 与 drain 控制消息的发送保障
- `internal/gateway/user_map.go`
  - 去掉同步 Redis 写路径
  - 增加 `SnapshotRouteConns`
- `internal/config/config.go`
  - 增加 cross instance / hybrid / 队列 / 状态机配置
- `internal/handler/message_handler.go`
  - 通过 `LifecycleGate` 将 HTTP `/msg/send` 纳入 `SendLease + sendClosed` 双阶段门禁
- `internal/handler/user_handler.go`
  - 增加 `user_ids <= 100` 校验
  - 在线状态降级时通过 `response.meta` 返回 `partial/data_source`
- `pkg/errcode/errcode.go`
  - 增加关闭期统一错误码
- `pkg/response/response.go`
  - 增加关闭期统一 HTTP status 映射 helper
  - 增加 `SuccessWithMeta(...)` 或等价 helper
- `internal/router/router.go`
  - 增加 `/ready` readiness 探针
- `cmd/server/main.go`
  - 初始化新组件与自动生成 `instance_id`
  - 调整关闭顺序

### 13.3 不改动

- `internal/service/message_service.go`
- `internal/gateway/protocol.go` 对外 WS 协议
- 消息表、会话表、seq 表结构

---

## 14. 迁移顺序

### 步骤 0：骨架与开关

- 新增组件文件与测试桩
- 接入进程启动自动生成 `instance_id`
- 默认 `cross_instance.enabled=false`

### 步骤 1：先收敛生命周期

- 落地 `internalCtx`
- 落地 `LifecycleGate` 与 `ready / routeable / ingressClosed / sendDraining / sendClosed / draining`
- 落地 `SendLease`，让 HTTP `/msg/send` 与 WS `WSSendMsg` 在进入 `MessageService` 前先完成准入
- 落地 `DrainReason` 与统一 `drain-exit` 编排
- 将 HTTP `/msg/send` 与 WS `WSSendMsg` 统一纳入 `LifecycleGate`
- 为 shutdown 错误补统一 response helper / HTTP status 映射
- 修正 `Shutdown()` 为“排空后摘实例”

### 步骤 2：拆出 Redis 写职责

- `UserMap` 移除同步 Redis online 写入
- 接入 `OnlineStateWriter`
- 接入 `routeWriteChan`
- 为 `register` 增加不阻塞 `eventLoop` 的 retry/repair fast path
- 增加实例拥有索引 `nexo:route:inst:{instanceId}`
- 收敛 `registerChan / unregisterChan` 在 shutdown 场景下的可靠性

### 步骤 3：实现 RouteStore 与校准

- 实现 register/unregister/query
- 实现 `alive=1 && routeable=1` 路由过滤
- 实现 `alive=1` 的 presence 聚合读取，不复用 push route 过滤
- 实现 user hash + instance index 双索引的 TTL 续期 loop 与 authoritative reconcile

### 步骤 4：实现 PushBus 与状态机

- 接入实例订阅 + 广播订阅
- publish / route-subscribe / broadcast-subscribe / push-route-read / presence-read 失败分别接入 capability 状态
- `RouteSubscribe=Degraded` 时联动 `DrainReasonSubscribeFault + MarkUnready + MarkUnrouteable + CloseIngress + BeginSendDrain + EnterDraining`
- `BroadcastSubscribe=Degraded` 时禁用 broadcast，而不是直接摘整实例
- 落地分批 `KickOnline` 迁移 worker 与通用 drain timeout

### 步骤 5：改造推送主路径

- `processPushTask` 拆分
- outbound publish 门禁只依赖 `Publish`
- 实现 route / broadcast
- 接入 `remoteEnvChan`
- broadcast 选择必须同时受 `BroadcastSubscribe` 能力约束
- 移除本期 `ExcludeConnId` 语义，保持发送连接自回显由客户端合并

### 步骤 6：在线状态与灰度

- `GetUsersOnlineStatus` 切 batch，并通过 `response.meta` 暴露 `partial/data_source`
- 明确 online 语义为“有 socket 连接”，验证 `RouteSubscribe=Degraded` 但连接尚未 kick/close 时仍返回在线
- 落地 `planned_upgrade` drain，并验证实例先退出 `/ready`、再在 `routeable` grace 窗口内完成连接迁移
- 验证新实例用新 `instance_id` 接管流量
- 先灰度 route
- 再打开群聊 broadcast
- 最后按阈值自动选择

---

## 15. 测试策略

### 15.1 单元测试

#### RouteStore

- register 写入正确
- unregister 删除正确
- 批量查询与解析正确
- `alive=1 && routeable=1` 过滤正确
- `alive=1` 的 presence 读取正确，且不受 `routeable=0` 影响
- `ready=0 && routeable=1` 的 planned/manual drain 中间态不会被误删为 stale route
- 惰性清理与 cleanup limit 生效
- 周期校准可回补漏写
- authoritative reconcile 可基于 instance index 删除本实例缺席连接的 stale field

#### PushBus

- instance channel 收发正确
- broadcast channel 收发正确
- `Close()` 后 publish 报错
- route/broadcast 运行时错误可分别回调给 capability 控制器
- dedup 表按 TTL/容量回收

#### HybridSelector

- 单聊始终 route
- 小群 route
- 大群 broadcast
- `force_broadcast=true` 时群聊强制 broadcast

#### WsServer

- 本地连接 + 远端连接时，本地优先投递，远端正确发布
- `BeginSendDrain()` 后新的 `HandleSendMsg` / HTTP `/msg/send` 被拒绝
- 已获取 `SendLease` 的发送请求可完整走完写库 + push 提交
- `CloseSendPath()` 后 `AsyncPushToUsers` 不再入队
- `planned_upgrade / subscribe_fault / manual` 都走统一 `drain-exit` 流程
- `planned/manual drain` 时实例立刻 `ready=0`，但在 routeable grace 窗口内仍允许既有连接接收远端 push
- `RouteSubscribe=Degraded` 时实例立刻 `ready=0 && routeable=0`，触发 `CloseIngress/BeginSendDrain/EnterDraining`，按批次 `KickOnline`，并在 timeout 后退出进程
- `RouteSubscribe=Degraded` 时仅允许排空中的 in-flight outbound publish 继续完成
- `BroadcastSubscribe=Degraded` 时禁用 broadcast，但实例不直接退出
- `BroadcastSubscribe=Degraded` 时本实例写入 `broadcast_ready=0`，其他 sender 涉及该实例时会强制回退为 route
- `Publish=Degraded` 时停止 outbound 跨实例发布
- `PushRouteRead=Degraded` 时统一退化为 local only + pull 补偿，但不阻断 `Publish`
- `PresenceRead=Degraded` 时仅影响在线状态接口，不阻断消息推送
- `RouteSubscribe=Degraded` 进入 fault-drain 后，到连接真正 `KickOnline/Close + unregister` 收敛前，presence 仍返回在线
- `register` 队列满时通过 repair fast path 补写，不出现永久漏路由，且不阻塞 `eventLoop`
- `unregister` 队列满时不阻塞 eventLoop
- `remoteEnvChan` 满时限时阻塞后丢弃
- broadcast 自投递被拦截
- 去重表生效
- `unregisterChan` 在 shutdown 期间不丢事件
- 写缓冲满时记录 drop，但连接保持存活
- `client_conn.go` 真实使用 `write_channel_size`
- `KickOnline` 采用控制消息优先 + flush-before-close 语义，而不是普通写队列 fire-and-forget

### 15.2 集成测试

- A/B 两实例单聊跨实例推送
- 小群 route 推送
- 大群 broadcast 推送
- `planned_upgrade` 时旧实例 `/ready` 失败、`routeable` 在 grace 窗口内保持为真、按批 drain，且新实例用新 `instance_id` 接管流量
- `RouteSubscribe` 故障时实例 `/ready` 失败、route 过滤立即生效，并开始 `fault-drain`
- `RouteSubscribe` 故障时连接按批迁移，且不会瞬时全量重连
- `BroadcastSubscribe` 故障时实例写入 `broadcast_ready=0`、仍保持 route-only，不发生整实例摘流量
- `PushRouteRead` 故障时 sender 不会再回退到广播，而是统一 local only + pull 补偿
- 滚动升级/实例重启后，新进程使用新 `instance_id`，旧 route 被正确过滤并收敛
- 关闭期间先停发送再排空，实例摘除后不再承载连接
- HTTP `/msg/send` 在关闭期被拒绝
- `GetUsersOnlineStatus` 合并本地 + 远端连接，并在降级时通过 `response.meta.partial=true` 返回
- `RouteSubscribe=Degraded` 但远端连接尚未收敛为 0 时，`GetUsersOnlineStatus` 仍返回在线
- route register 丢失后，周期校准可恢复
- 发送连接收到自己的 push 时，客户端按 `client_msg_id` 或 `server_msg_id/seq` 合并，不重复展示

### 15.3 回归测试

- 现有单实例 websocket 测试全部通过
- auth/group/message/conversation 不回归

---

## 16. 验收标准

### 功能验收

- [ ] 单聊跨实例推送正常
- [ ] 小群跨实例推送正常
- [ ] 大群跨实例推送正常
- [ ] 同一用户多端跨实例全部收到消息
- [ ] 广播模式发送者实例不重复投递
- [ ] 发送连接可能收到自己的 push，但客户端可按 `client_msg_id` 或 `server_msg_id/seq` 合并，不重复展示
- [ ] `GetUsersOnlineStatus` 正确聚合本地和远端连接
- [ ] 在线状态在远端查询失败时通过 `response.meta` 显式返回 `partial=true`
- [ ] `cross_instance.enabled=false` 时行为与现网一致

### 稳态验收

- [ ] `register` 不会因队列满形成永久漏路由
- [ ] `unregister` 队列满不阻塞 eventLoop
- [ ] 路由缺失可被周期校准修复
- [ ] capability 状态在 publish/route-subscribe/broadcast-subscribe/push-route-read/presence-read 故障时分别进入 `Degraded`
- [ ] `planned/manual drain` 时实例先退出 `ready`，但在 routeable grace 窗口内既有连接仍可接收远端实时 push
- [ ] `RouteSubscribe=Degraded` 时实例立刻退出 `ready` 和 `routeable` 状态并开始 drain
- [ ] `RouteSubscribe=Degraded` 时连接按批迁移，并在限定时间内完成收敛
- [ ] `BroadcastSubscribe=Degraded` 时实例退化为 route-only，而不是直接退出
- [ ] `BroadcastSubscribe=Degraded` 时实例会镜像 `broadcast_ready=0`，涉及该实例的广播发送统一回退为 route
- [ ] `PushRouteRead=Degraded` 时 sender 不会退化到广播，而是统一 local only + pull 补偿
- [ ] 滚动升级/实例重启后不会复用旧 `instance_id`
- [ ] `Publish / BroadcastSubscribe / PushRouteRead / PresenceRead` 在恢复探活后可回到 `Ready`
- [ ] `RouteSubscribe=Degraded` 的当前进程不会 in-place 回到 `Ready`，而是 drain 后退出

### 生命周期验收

- [ ] `HandleSendMsg` 在关闭期被明确拒绝，而不是写库后静默不推
- [ ] HTTP `/msg/send` 在关闭期被明确拒绝，而不是写库后静默不推
- [ ] 已在关闭前获得 `SendLease` 的发送请求不会因为 gate 翻转而出现“写库成功但推送未提交”
- [ ] `Shutdown()` 在摘实例前完成 push/remote/routeWrite 排空
- [ ] `Shutdown()` 在返回前完成 `unregister` 收敛
- [ ] `RouteSubscribe=Degraded` 进入 drain 后，在线状态在连接实际收敛前不被误判为离线
- [ ] 实例摘除时不再承载本地连接
- [ ] 关闭后无遗留 goroutine

### 性能验收

- [ ] 单聊跨实例延迟 < 100ms
- [ ] 大群广播的 gateway 分发增量延迟 < 200ms（不含群成员枚举与成员查询耗时）
- [ ] 100 用户在线状态批量查询 < 10ms
- [ ] 慢连接场景允许少量 push drop，但连接不因单次写满被主动断开

---

## 17. 后续优化方向

以下内容不纳入本期：

1. 基于在线规模缓存的“免查路由粗判广播”
2. Redis Stream 替代 Pub/Sub
3. 广播按地域或实例组分域
4. 推送批处理与合包
5. 用路由表完全替代 legacy online key
