# Nexo IM 系统架构设计方案

## 1. 问题陈述

基于 open-im-server 架构，实现一个简化的 IM 系统，支持单聊、群聊基本功能。技术栈调整：

- 存储：MongoDB → GORM + MySQL
- HTTP框架：Gin → Hertz
- 鉴权：JWT Token
- 长连接：gorilla/websocket + hertz adaptor (https://www.cloudwego.io/zh/docs/hertz/tutorials/third-party/protocol/websocket/)

## 2. 当前状态分析

### 2.1 open-im-server 核心架构

open-im-server 采用微服务架构，包含以下核心模块：

- **msggateway**: WebSocket 网关，管理客户端长连接
- **msg**: 消息处理服务，负责消息发送、存储、拉取
- **auth**: 认证服务，Token 管理
- **user/group/conversation**: 用户、群组、会话管理
  核心消息流程：

1. Client → WebSocket → msggateway → Kafka MQ → msgtransfer → MongoDB
2. 推送：msg → msggateway → WebSocket → Client

### 2.2 简化策略

由于是简化版，采用**单体架构**，去除：

- gRPC 内部通信 → 直接函数调用
- Kafka MQ → 直接写入 MySQL
- 服务发现 (etcd/k8s) → 单实例部署

## 3. 提议的架构设计

### 3.1 整体架构图

```warp-runnable-command
┌─────────────────────────────────────────────────────────────┐
│                        Nexo IM Server                       │
├─────────────────────────────────────────────────────────────┤
│  ┌───────────────┐    ┌───────────────┐                    │
│  │  Hertz HTTP   │    │   WebSocket   │                    │
│  │   API Layer   │    │    Gateway    │                    │
│  └───────┬───────┘    └───────┬───────┘                    │
│          │                    │                            │
│          └────────┬───────────┘                            │
│                   │                                        │
│          ┌────────▼────────┐                               │
│          │   JWT Auth MW   │                               │
│          └────────┬────────┘                               │
│                   │                                        │
│  ┌────────────────┼────────────────┐                       │
│  │                │                │                       │
│  ▼                ▼                ▼                       │
│ ┌────────┐  ┌───────────┐  ┌─────────────┐                │
│ │  User  │  │   Group   │  │   Message   │                │
│ │Service │  │  Service  │  │   Service   │                │
│ └────┬───┘  └─────┬─────┘  └──────┬──────┘                │
│      │            │               │                        │
│      └────────────┼───────────────┘                        │
│                   │                                        │
│          ┌────────▼────────┐                               │
│          │  Repository     │                               │
│          │    Layer        │                               │
│          └────────┬────────┘                               │
│                   │                                        │
│          ┌────────▼────────┐    ┌─────────────┐           │
│          │   GORM + MySQL  │    │    Redis    │           │
│          └─────────────────┘    └─────────────┘           │
└─────────────────────────────────────────────────────────────┘
```

### 3.2 项目目录结构

```warp-runnable-command
nexo/
├── cmd/
│   └── server/
│       └── main.go              # 程序入口
├── config/
│   └── config.yaml              # 配置文件
├── internal/
│   ├── config/                  # 配置加载
│   │   └── config.go
│   ├── entity/                  # 领域实体 (基于 entity_define.txt)
│   │   ├── user.go
│   │   ├── group.go
│   │   ├── message.go
│   │   ├── conversation.go
│   │   └── seq.go
│   ├── repository/              # 数据访问层
│   │   ├── user_repo.go
│   │   ├── group_repo.go
│   │   ├── message_repo.go
│   │   ├── conversation_repo.go
│   │   └── seq_repo.go
│   ├── service/                 # 业务逻辑层
│   │   ├── auth_service.go
│   │   ├── user_service.go
│   │   ├── group_service.go
│   │   ├── message_service.go
│   │   └── conversation_service.go
│   ├── handler/                 # HTTP API 处理
│   │   ├── auth_handler.go
│   │   ├── user_handler.go
│   │   ├── group_handler.go
│   │   └── message_handler.go
│   ├── gateway/                 # WebSocket 网关
│   │   ├── ws_server.go
│   │   ├── client.go
│   │   ├── user_map.go
│   │   └── message_handler.go
│   ├── middleware/              # 中间件
│   │   ├── auth.go
│   │   └── cors.go
│   └── router/                  # 路由注册
│       └── router.go
├── pkg/
│   ├── jwt/                     # JWT 工具
│   │   └── jwt.go
│   ├── response/                # 统一响应
│   │   └── response.go
│   ├── constant/                # 常量定义
│   │   └── constant.go
│   └── errcode/                 # 错误码
│       └── errcode.go
├── go.mod
└── go.sum
```

### 3.3 数据库设计 (MySQL)

基于 entity_define.txt，设计以下表结构：

#### 3.3.1 用户表 (users)

```SQL
CREATE TABLE users (
    id VARCHAR(64) PRIMARY KEY,
    nickname VARCHAR(128) NOT NULL DEFAULT '',
    avatar VARCHAR(512) DEFAULT '',
    extra JSON,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

#### 3.3.2 群组表 (groups)

```SQL
CREATE TABLE `groups` (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    introduction VARCHAR(512) DEFAULT '',
    avatar VARCHAR(512) DEFAULT '',
    extra JSON,
    status INT DEFAULT 0,
    creator_user_id VARCHAR(64) NOT NULL,
    group_type INT DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    INDEX idx_creator (creator_user_id),
    INDEX idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

#### 3.3.3 群成员表 (group_members)

```SQL
CREATE TABLE group_members (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    group_id VARCHAR(64) NOT NULL,
    user_id VARCHAR(64) NOT NULL,
    group_nickname VARCHAR(128) DEFAULT '',
    group_avatar VARCHAR(512) DEFAULT '',
    extra JSON,
    role_level INT DEFAULT 0,
    status INT NOT NULL DEFAULT 0, -- 成员状态：0=normal, 1=left, 2=kicked
    joined_at BIGINT NOT NULL,
    join_seq BIGINT NOT NULL DEFAULT 0, -- 入群后可见的起始消息 seq（用于"加入群不可看历史"）
    inviter_user_id VARCHAR(64) DEFAULT '',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE KEY uk_group_user (group_id, user_id),
    INDEX idx_group_status (group_id, status),
    INDEX idx_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

**重新入群处理**：用户退群后重新加入时，使用 `ON DUPLICATE KEY UPDATE` 更新而非插入，避免唯一约束冲突：

```sql
INSERT INTO group_members (group_id, user_id, status, joined_at, join_seq, ...)
VALUES (?, ?, 0, ?, ?, ...)
ON DUPLICATE KEY UPDATE
    status = 0,
    joined_at = VALUES(joined_at),
    join_seq = VALUES(join_seq),
    updated_at = VALUES(updated_at);
```

#### 3.3.4 会话表 (conversations)

```SQL
CREATE TABLE conversations (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    conversation_id VARCHAR(256) NOT NULL,
    owner_id VARCHAR(64) NOT NULL,
    conversation_type INT NOT NULL,
    peer_user_id VARCHAR(64) DEFAULT '',
    group_id VARCHAR(64) DEFAULT '',
    recv_msg_opt INT DEFAULT 0,
    is_pinned TINYINT(1) DEFAULT 0,
    extra JSON,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE KEY uk_owner_conv (owner_id, conversation_id),
    INDEX idx_owner (owner_id),
    INDEX idx_conv_type (conversation_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

#### 3.3.5 会话序列号表 (seq_conversations)

```SQL
CREATE TABLE seq_conversations (
    conversation_id VARCHAR(256) PRIMARY KEY,
    max_seq BIGINT DEFAULT 0,
    min_seq BIGINT DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

#### 3.3.6 用户序列号表 (seq_users)

```SQL
CREATE TABLE seq_users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    conversation_id VARCHAR(256) NOT NULL,
    min_seq BIGINT DEFAULT 0,
    max_seq BIGINT DEFAULT 0,
    read_seq BIGINT DEFAULT 0,
    UNIQUE KEY uk_user_conv (user_id, conversation_id),
    INDEX idx_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

#### 3.3.7 消息表 (messages)

```SQL
CREATE TABLE messages (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    conversation_id VARCHAR(256) NOT NULL,
    seq BIGINT NOT NULL,
    client_msg_id VARCHAR(64) NOT NULL, -- 客户端生成的幂等ID（重试必须复用）
    sender_id VARCHAR(64) NOT NULL,
    recv_id VARCHAR(64) DEFAULT '',
    group_id VARCHAR(64) DEFAULT '',
    session_type INT NOT NULL,
    msg_type INT NOT NULL,
    content_text TEXT,
    content_image VARCHAR(512) DEFAULT '',
    content_video VARCHAR(512) DEFAULT '',
    content_audio VARCHAR(512) DEFAULT '',
    content_file VARCHAR(512) DEFAULT '',
    content_custom JSON,
    extra JSON,
    send_at BIGINT NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    UNIQUE KEY uk_conv_seq (conversation_id, seq),
    UNIQUE KEY uk_sender_client_msg (sender_id, client_msg_id),
    INDEX idx_sender (sender_id),
    INDEX idx_send_at (send_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### 3.4 核心模块设计

#### 3.4.1 JWT 鉴权模块

```go
// pkg/jwt/jwt.go
type Claims struct {
    UserId     string `json:"user_id"`
    PlatformId int    `json:"platform_id"`
    jwt.RegisteredClaims
}
func GenerateToken(userId string, platformId int, secret string, expire time.Duration) (string, error)
func ParseToken(tokenString, secret string) (*Claims, error)
```

#### 3.4.2 WebSocket Gateway 设计 (参考 open-im-server/internal/msggateway)

核心组件：

- **WsServer**: WebSocket 服务器，管理连接生命周期
- **Client**: 单个客户端连接的抽象
- **UserMap**: 用户ID到连接的映射，支持多端登录

连接与读写模型（对齐 open-im-server 的底线能力）：

- **鉴权握手**：从 query（或 `v` 参数）解析 `token/send_id/platform_id/operation_id`，解析 Token 并校验 `token.user_id == send_id` 且 `token.platform_id == platform_id`。
- **单连接写串行化**：每个连接维护 `send chan` + 单独 `writeLoop`，所有推送/ACK 都入队，避免并发写导致的乱序/崩溃。
- **写超时**：每次写设置 `write deadline`；写超时/写失败直接关闭连接，依赖客户端重连后按 seq 拉取补齐。
- **心跳保活**：`ping/pong` + `read deadline`，及时清理断链连接。
- **慢消费者策略**：`send chan` 满（或积压超过阈值）时，踢下线/断开（推荐），或按策略丢弃可重建的 push（但必须保证客户端会定期拉取）。
- **命名约定**：Go 结构体字段使用 `Id`（如 `UserId`），对外 JSON 字段使用 `snake_case`（如 `user_id/err_msg`）。

```go
// internal/gateway/ws_server.go
type WsServer struct {
    clients      *UserMap           // userId -> []*Client
    register     chan *Client
    unregister   chan *Client
    msgService   *service.MessageService
}
func (s *WsServer) HandleConnection(ctx context.Context, conn *websocket.Conn, userId string, platformId int)
func (s *WsServer) PushToUser(userId string, msg *entity.Message) error
func (s *WsServer) PushToGroup(groupId string, msg *entity.Message) error
```

WebSocket 消息协议：

```go
// 请求消息格式
type WSRequest struct {
    ReqIdentifier int32  `json:"req_identifier"` // 请求类型
    MsgIncr       string `json:"msg_incr"`       // 可选：客户端递增序号/追踪字段
    OperationId   string `json:"operation_id"`   // 操作ID
    Token         string `json:"token"`          // 可选：与握手 token 一致（兼容 open-im）
    SendId        string `json:"send_id"`        // 发送者ID
    Data          []byte `json:"data"`           // 业务数据
}
// 响应消息格式
type WSResponse struct {
    ReqIdentifier int32  `json:"req_identifier"`
    MsgIncr       string `json:"msg_incr"`
    OperationId   string `json:"operation_id"`
    ErrCode       int    `json:"err_code"`
    ErrMsg        string `json:"err_msg"`
    Data          []byte `json:"data"`
}
// 请求类型常量
const (
    // 对齐 open-im-server/internal/msggateway/constant.go（仅实现子集）
    WSGetNewestSeq     = 1001 // 获取最新 seq
    WSPullMsgBySeqList = 1002 // 按 seq 拉取
    WSSendMsg          = 1003 // 发送消息
    WSPullMsg          = 1005 // 拉取消息（可选）
    WSGetConvMaxReadSeq = 1006 // 获取会话已读/最大 seq（可选）
    WSPushMsg          = 2001 // 服务端推送
)
```

#### 3.4.3 消息服务设计

消息发送流程：

1. **权限校验**（群消息必须）：校验发送者是否为群成员且状态正常
2. **幂等校验**：根据 `(sender_id, client_msg_id)` 判断是否为重试请求
3. **分配 seq**：通过 Redis INCR 为当前 conversation 分配单调递增 `seq`
4. **持久化**：写入 `messages`，同步更新 `seq_conversations.max_seq`
5. **ACK + 异步推送**：事务提交后立即返回 ACK，通过异步推送队列推送给接收方

```go
// internal/service/message_service.go
type MessageService struct {
    msgRepo    *repository.MessageRepo
    seqRepo    *repository.SeqRepo
    convRepo   *repository.ConversationRepo
    groupRepo  *repository.GroupRepo
    wsServer   *gateway.WsServer
    rdb        *redis.Client
}

// 发送单聊消息
func (s *MessageService) SendSingleMessage(ctx context.Context, senderId, recvId, clientMsgId string, content entity.MessageContent, msgType int32) (*entity.Message, error)

// 发送群聊消息（含权限校验）
func (s *MessageService) SendGroupMessage(ctx context.Context, senderId, groupId, clientMsgId string, content entity.MessageContent, msgType int32) (*entity.Message, error) {
    // 1. 权限校验：发送者必须是群成员且状态正常
    member, err := s.groupRepo.GetMember(ctx, groupId, senderId)
    if err != nil || member == nil {
        return nil, ErrNotGroupMember
    }
    if member.Status != 0 {
        return nil, ErrMemberNotActive // 已退群或被踢
    }

    // 2. 校验群状态
    group, err := s.groupRepo.Get(ctx, groupId)
    if err != nil || group.Status != 0 {
        return nil, ErrGroupNotAvailable
    }

    // 3. 后续消息处理流程...
}

// 拉取消息 (支持分页，强制 limit <= 100)
func (s *MessageService) PullMessages(ctx context.Context, userId, conversationId string, beginSeq, endSeq int64, limit int) ([]*entity.Message, int64, error) {
    // 强制分页限制
    if limit <= 0 || limit > 100 {
        limit = 100
    }
    // ...
}
```

**群消息权限校验（P0 安全要求）**：

发送群消息前必须校验：
1. **发送者是群成员**：`group_members` 表中存在 `(group_id, user_id)` 记录
2. **成员状态正常**：`group_members.status = 0`（非 1=已退群, 2=被踢）
3. **群状态正常**：`groups.status = 0`（未解散/禁用）

```go
// 权限校验示例
func (s *MessageService) checkGroupPermission(ctx context.Context, groupId, senderId string) error {
    member, err := s.groupRepo.GetMember(ctx, groupId, senderId)
    if err != nil {
        return ErrNotGroupMember
    }
    if member.Status != 0 {
        return ErrMemberNotActive
    }

    group, err := s.groupRepo.Get(ctx, groupId)
    if err != nil || group.Status != 0 {
        return ErrGroupNotAvailable
    }
    return nil
}
```

消息幂等（client_msg_id）：

- **为什么需要**：弱网/断线重连/超时重试时，客户端无法判断服务端是否已落库，只能重发；若不做幂等会导致重复消息、重复占用 seq、重复推送。
- **客户端生成**：每条新消息生成一次 `client_msg_id`（推荐 `UUIDv4/ULID`），同一条消息的重试必须复用该值。
- **唯一性约定**：同一 `sender_id` 下的 `client_msg_id` 必须全局唯一（不可跨会话复用）；否则会被服务端视为同一条消息的重试。
- **服务端落库约束**：`messages` 表增加 `client_msg_id`，并建立 `UNIQUE(sender_id, client_msg_id)`。
- **服务端处理策略**：
  - 发送时先查 `(sender_id, client_msg_id)` 是否存在：存在则直接返回已有消息（同一个 `seq`），不产生二次落库副作用；可按需再次触发 push（不影响一致性，客户端按 `(conversation_id, seq)` 去重）。
  - 并发重试竞态下如果插入触发唯一键冲突：捕获后再查询并返回已有消息，实现"至少一次请求、恰好一次落库"的效果。

消息 Seq 生成策略（Redis INCR）：

使用 Redis INCR 原子操作分配 seq，避免 MySQL 行锁热点：

```go
// internal/repository/seq_repo.go
type SeqRepo struct {
    rdb *redis.Client
    db  *gorm.DB
}

// AllocSeq 通过 Redis INCR 分配 seq
func (r *SeqRepo) AllocSeq(ctx context.Context, conversationId string) (int64, error) {
    key := fmt.Sprintf("seq:conv:%s", conversationId)
    seq, err := r.rdb.Incr(ctx, key).Result()
    if err != nil {
        return 0, err
    }
    return seq, nil
}

// SyncSeqToMySQL 定期将 Redis seq 同步到 MySQL（用于持久化备份和恢复）
func (r *SeqRepo) SyncSeqToMySQL(ctx context.Context, conversationId string, maxSeq int64) error {
    return r.db.Model(&entity.SeqConversation{}).
        Where("conversation_id = ?", conversationId).
        Update("max_seq", maxSeq).Error
}

// InitSeqFromMySQL 服务启动时从 MySQL 恢复 seq 到 Redis
func (r *SeqRepo) InitSeqFromMySQL(ctx context.Context, conversationId string) error {
    var seqConv entity.SeqConversation
    if err := r.db.Where("conversation_id = ?", conversationId).First(&seqConv).Error; err != nil {
        return err
    }
    key := fmt.Sprintf("seq:conv:%s", conversationId)
    return r.rdb.Set(ctx, key, seqConv.MaxSeq, 0).Err()
}
```

**seq 一致性保证**：
- 消息落库时，同一事务内更新 `seq_conversations.max_seq`
- Redis seq 定期（如每 100 条或每 10 秒）同步到 MySQL
- 服务重启时从 MySQL 恢复 Redis seq（取 `MAX(redis_seq, mysql_max_seq)`）

#### 3.4.4 会话服务设计

ConversationId 生成规则 (参考 open-im-server):

- 单聊: `si_{min(userA,userB)}_{max(userA,userB)}`
- 群聊: `sg_{groupId}`
- 说明：当 `user_id` 取 `VARCHAR(64)` 时，单聊 `conversation_id` 最长约 `3+64+1+64=132`，因此数据库统一使用 `VARCHAR(256)` 以避免溢出。

```go
// internal/service/conversation_service.go
func GenSingleConversationId(userA, userB string) string
func GenGroupConversationId(groupId string) string
```

#### 3.4.5 关键约定与一致性策略（MVP）

1. ConversationId 约定

- ConversationId 作为分区键与同步维度，**一旦生成不可变更**。
- 单聊 ConversationId 必须对双方稳定一致（按 userId 排序拼接）。

2. “加入群不可看历史”

- 规则：用户加入群后，只能拉取/接收加入之后产生的群消息。
- 落库方式：
  - `group_members.join_seq`：入群后可见起始 seq
  - `seq_users.min_seq`：该用户在该会话可见的最小 seq（拉取时必须截断）
- 入群关键流程（事务内完成）：
  1. 锁住 `seq_conversations(conversation_id=sg_{groupId}) FOR UPDATE`
  2. 读取当前 `max_seq`，计算 `join_seq = max_seq + 1`
  3. 写入 `group_members.join_seq = join_seq`
  4. Upsert `seq_users(user_id, conversation_id)`：`min_seq=join_seq, read_seq=join_seq-1`
- 拉取/同步时强制边界（对齐 open-im-server 的 min/max 语义）：
  - `beginSeq = max(beginSeq, seq_users.min_seq)`
  - `endSeq = min(endSeq, seq_conversations.max_seq)`
  - 若 `seq_users.max_seq > 0`：`endSeq = min(endSeq, seq_users.max_seq)`（用于退群/被踢后的“可见上界”）

3. 退群/被踢后不可看新消息（对齐 open-im-server）

- 群成员退出/被踢时，将 `seq_users.max_seq` 置为当前 `seq_conversations.max_seq`，作为该用户在该群会话的“可见上界”。
- 拉取/同步时强制：若 `seq_users.max_seq > 0`，则 `endSeq` 不得超过该上界；推送时也不再推送给 `group_members.status != 0` 的用户。

4. 已读游标单调性

- `seq_users.read_seq` 必须单调递增；更新使用 `read_seq = GREATEST(read_seq, newReadSeq)`，避免多端/并发回退。

5. 推送语义

- 服务端推送为 at-least-once，客户端展示层需按 `(conversation_id, seq)` 去重，避免网络抖动导致的重复 push 展示。

### 3.5 API 设计

#### 3.5.1 HTTP API (Hertz)

**认证接口**

- `POST /auth/login` - 用户登录，返回 JWT Token
- `POST /auth/refresh` - 刷新 Token
  **用户接口**
- `POST /user/register` - 用户注册
- `GET /user/info` - 获取用户信息
- `PUT /user/update` - 更新用户信息
  **群组接口**
- `POST /group/create` - 创建群组
- `POST /group/join` - 加入群组
- `POST /group/quit` - 退出群组
- `GET /group/info` - 获取群组信息
- `GET /group/members` - 获取群成员列表
  **消息接口**
- `POST /msg/send` - 发送消息 (HTTP 备用通道)
- `GET /msg/pull` - 拉取历史消息
  **会话接口**
- `GET /conversation/list` - 获取会话列表
- `PUT /conversation/update` - 更新会话设置

#### 3.5.2 WebSocket API

连接地址（对外字段使用 `snake_case`）：`ws://host:port/ws?token={jwt_token}&send_id={user_id}&platform_id={platform_id}&operation_id={opid}&sdk_type={go|js}&is_msg_resp=true`
消息操作通过 WSRequest/WSResponse 协议进行。

注意：

- 服务端以 JWT 中的 `user_id` 作为真实发送者身份，**不信任** WSRequest 里的 `send_id` 字段（可用于调试/兼容，但必须校验一致）。
- `WSSendMsg` 的业务数据需包含 `client_msg_id` 以支持幂等。
- 内部采用单体/微服务不影响对外协议；若希望复用 open-im SDK，需要兼容 open-im 的字段命名（如 `sendID/platformID/operationID/msgIncr/reqIdentifier/errCode/errMsg`）以及 `Data` 的 protobuf，本方案 Phase 1 采用 `snake_case`，建议通过网关双字段兼容或 fork SDK 适配。

### 3.6 消息推送流程

#### 3.6.0 异步推送设计

消息落库与推送解耦，使用 goroutine + channel 实现异步推送，避免推送耗时阻塞处理链路：

```go
// internal/gateway/ws_server.go
type PushTask struct {
    Msg       *entity.Message
    TargetIds []string  // 目标用户 ID 列表
    ExcludeId string    // 排除的连接（如发送者当前连接）
}

type WsServer struct {
    pushChan chan *PushTask  // 推送任务队列，缓冲 10000
    // ...
}

func NewWsServer() *WsServer {
    s := &WsServer{
        pushChan: make(chan *PushTask, 10000),
    }
    go s.pushLoop()  // 启动推送协程
    return s
}

// 推送协程：消费推送任务
func (s *WsServer) pushLoop() {
    for task := range s.pushChan {
        for _, userId := range task.TargetIds {
            s.pushToUserConnections(userId, task.Msg, task.ExcludeId)
        }
    }
}

// 异步提交推送任务（非阻塞）
func (s *WsServer) AsyncPush(msg *entity.Message, targetIds []string, excludeId string) {
    select {
    case s.pushChan <- &PushTask{Msg: msg, TargetIds: targetIds, ExcludeId: excludeId}:
        // 成功入队
    default:
        // 队列满，记录 metrics，依赖客户端拉取补齐
        log.Warn("push channel full, msg dropped", "conv_id", msg.ConversationId, "seq", msg.Seq)
    }
}
```

**消息发送流程（含异步推送）**：
1. 幂等校验 → 2. 权限校验（群消息） → 3. Redis INCR 分配 seq → 4. 持久化 → 5. 返回 ACK → 6. 异步推送（非阻塞）

#### 3.6.1 单聊消息流程

```warp-runnable-command
Client A (发送方)
    │
    ▼ WSSendMsg
┌───────────────┐
│  WsServer     │
│  handleMsg()  │
└───────┬───────┘
        │
        ▼
┌───────────────┐
│MessageService │
│SendSingleMsg()│
└───────┬───────┘
        │
        ▼
┌───────────────┐
│  MySQL        │
│  (持久化)      │
└───────┬───────┘
        │
        ├──────────────► WSResponse(ACK) → Client A (当前连接)
        ├──────────────► WSPushMsg → Client B (所有在线连接)
        └──────────────► WSPushMsg → Client A (其他在线连接，用于多端同步)
```

关键补充（对齐 open-im-server 的核心语义）：

- **ACK 必须存在**：`WSSendMsg` 需要返回 ACK（包含 `conversation_id/seq/server_msg_id/send_at` 等），用于客户端停止重试与更新发送状态。
- **幂等位置**：在分配 `seq` 与落库之前先做 `(sender_id, client_msg_id)` 幂等判断；重复请求直接返回同一条消息的 ACK，避免重复占用 `seq`/重复落库；可按需再次触发 push（客户端去重即可）。
- **多连接同步**：因 MVP 允许同账号多连接在线，除推送给接收方外，还需推送给发送方的“其他连接”（一般可排除当前连接，避免 ACK+Push 双份）。
- **会话/未读来源**：`seq_conversations.max_seq` 在落库事务内更新；发送方 `seq_users.read_seq` 可推进到本次 `seq`（发送方无未读），接收方 `read_seq` 不变（未读增长）。

#### 3.6.2 群聊消息流程

```warp-runnable-command
Client A (发送方)
    │
    ▼ WSSendMsg
┌───────────────┐
│  WsServer     │
└───────┬───────┘
        │
        ▼
┌───────────────┐
│MessageService │
│SendGroupMsg() │
└───────┬───────┘
        │
        ▼
┌───────────────┐
│  MySQL        │
│  (持久化)      │
└───────┬───────┘
        │
        ├──────────────► WSResponse(ACK) → Client A (当前连接)
        │
        ├──────────────► GroupRepo.GetMembers()（过滤 `group_members.status!=0` 的成员）
        │
        └──────────────► WsServer 批量推送 WSPushMsg
                           - → 群成员在线连接（含发送方其他连接）
                           - 不推送给非群成员/已退群用户
```

关键补充（加入群不可看历史 + 并发边界）：

- **入群不可看历史**：每个成员在入群时写入 `group_members.join_seq`，并同步到 `seq_users.min_seq`；拉取时强制 `beginSeq >= min_seq`，保证历史不可见。
- **退群/被踢不可看新消息**：退出/被踢时写入 `seq_users.max_seq = current_max_seq`；拉取时强制 `endSeq <= max_seq`，推送时不再推送给 `status!=0` 的用户。
- **join_seq 与发消息并发一致性**：入群与发群消息都需要锁同一行 `seq_conversations(conversation_id=sg_{groupId}) FOR UPDATE`：
  - 入群先锁：`join_seq=max_seq+1`，之后产生的消息 `seq>=join_seq` 可见
  - 发消息先锁：先产生的消息 `seq<=max_seq` 对后入群用户不可见
- **幂等与推送**：同单聊，重复请求只保证不重复落库/不重复占用 `seq`；允许重复 push（客户端按 `(conversation_id, seq)` 去重展示）。

### 3.7 消息队列（Phase 2 预留）

Phase 1（当前方案）保持 **MySQL 直写（DB-first）**：写库成功即返回 ACK，并尽力通过 WebSocket 实时推送。

- 实时 push 允许丢失/重复（客户端按 `(conversation_id, seq)` 去重 + 按 seq 拉取补齐）。
- 若发生“事务提交成功但 push 前进程崩溃”，会丢一次实时 push，但消息已落库，客户端可通过 `WSGetNewestSeq` + `WSPullMsgBySeqList/WSPullMsg` 补齐。

Phase 2（预留）再引入消息队列，参考 open-im-server 的解耦方式改为 **MQ-first**（先入队再落库/推送）：

- **Redis Streams**：按 `conversation_id` 做分片（`hash(conversation_id)%N`）写入不同 stream，保证同会话有序消费；消费组 `XREADGROUP`，处理成功后 `XACK`。
- **幂等与去重**：消费端仍以 `(sender_id, client_msg_id)` 做幂等落库，防止重投/重试导致重复消息。
- **可切换中间件**：可用 Watermill 这类库封装 `Publish/Consume/Ack`，后续切换到 Kafka/NATS 等时尽量不改业务代码。

### 3.8 Redis 缓存设计

| Key Pattern                       | 用途               | TTL  |
| --------------------------------- | ------------------ | ---- |
| `token:{user_id}:{platform_id}`   | JWT Token 存储     | 7d   |
| `online:{user_id}`                | 用户在线状态       | 60s  |
| `online:conns:{user_id}`          | 用户在线连接列表   | 60s  |
| `user:{user_id}`                  | 用户信息缓存       | 5min |
| `group:members:{group_id}`        | 群成员列表缓存     | 5min |
| `seq:conv:{conversation_id}`      | 会话 seq 计数器    | 永久 |

**用户在线状态（Redis 存储）**：

使用 Redis 存储用户在线状态，支持多实例部署场景下的状态共享：

```go
// 用户上线时
func (m *UserMap) Register(ctx context.Context, userId string, client *Client) {
    // 1. 本地内存存储连接
    m.local.Set(userId, client)
    // 2. Redis 记录在线状态
    m.rdb.Set(ctx, fmt.Sprintf("online:%s", userId), "1", 60*time.Second)
    // 3. 启动心跳续期 goroutine
}

// 用户下线时
func (m *UserMap) Unregister(ctx context.Context, userId string, client *Client) {
    m.local.Delete(userId, client)
    // 检查该用户是否还有其他连接，无则删除在线状态
    if !m.local.HasConnections(userId) {
        m.rdb.Del(ctx, fmt.Sprintf("online:%s", userId))
    }
}

// 检查用户是否在线
func (m *UserMap) IsOnline(ctx context.Context, userId string) bool {
    return m.rdb.Exists(ctx, fmt.Sprintf("online:%s", userId)).Val() > 0
}
```

**群成员缓存主动失效**：

群成员变更时（入群/退群/被踢）必须主动删除缓存，避免推送错误：

```go
// internal/repository/group_repo.go
func (r *GroupRepo) AddMember(ctx context.Context, member *entity.GroupMember) error {
    // 使用 ON DUPLICATE KEY UPDATE 处理重新入群
    err := r.db.Exec(`INSERT INTO group_members (...) VALUES (...)
        ON DUPLICATE KEY UPDATE status=0, joined_at=?, join_seq=?`, ...).Error
    if err != nil {
        return err
    }
    // 主动删除群成员缓存
    r.rdb.Del(ctx, fmt.Sprintf("group:members:%s", member.GroupId))
    return nil
}

func (r *GroupRepo) RemoveMember(ctx context.Context, groupId, userId string, status int) error {
    err := r.db.Model(&entity.GroupMember{}).
        Where("group_id = ? AND user_id = ?", groupId, userId).
        Update("status", status).Error
    if err != nil {
        return err
    }
    // 主动删除群成员缓存
    r.rdb.Del(ctx, fmt.Sprintf("group:members:%s", groupId))
    return nil
}
```

后续增强（Phase 2）：
- **本地缓存 + 事件驱动失效**：群成员/群信息使用进程内 `localcache`，通过 Redis Pub/Sub 广播失效事件
- **群推送并发限流**：群消息 fanout 使用 worker pool + 最大并发

## 4. 关键技术决策

| 决策点       | 选择                                                      | 原因                                                           |
| ------------ | --------------------------------------------------------- | -------------------------------------------------------------- |
| 架构模式     | 单体                                                      | 简化部署和运维，适合初期开发                                   |
| HTTP框架     | Hertz                                                     | 字节跳动开源，高性能，生态完善                                 |
| ORM          | GORM                                                      | Go 生态最成熟的 ORM                                            |
| 消息存储     | MySQL                                                     | 简化运维，GORM 支持好                                          |
| 消息推送     | 异步推送（goroutine + channel）+ Redis 在线状态           | 落库与推送解耦，避免阻塞；Redis 存储在线状态支持多实例扩展      |
| 群历史可见性 | 入群不可看历史                                            | 产品规则明确，避免历史消息泄漏                                 |
| 退群可见性   | `seq_users.max_seq` 上界                                  | 对齐 open-im：退群/被踢后不可看新消息                          |
| 多端踢下线   | 暂不做                                                    | MVP 简化，后续可在 Auth 模块扩展                               |
| 消息幂等     | client_msg_id + 唯一索引                                  | 避免重试导致重复落库/重复占用 seq；允许重复 push（客户端去重） |
| Seq 生成     | Redis INCR                                                | 避免 MySQL 行锁热点，高并发性能好；定期同步到 MySQL 做持久化备份    |
| 推送重试     | Phase 1 不做（依赖拉取补齐）                              | 降低系统复杂度，保证核心一致性                                 |
| 消息队列     | Phase 2 引入 Redis Streams（MQ-first），可 Watermill 切换 | 预留扩展能力，避免业务绑死中间件                               |

## 5. 实施步骤

### Phase 1: 基础框架 (2-3天)

- 项目初始化，配置管理
- MySQL/Redis 连接
- GORM Entity 定义
- JWT 鉴权模块
- Hertz 路由框架搭建

### Phase 2: 用户与群组 (2-3天)

- 用户注册/登录
- 用户信息 CRUD
- 群组创建/加入/退出
- 群成员管理
- 入群 `join_seq/min_seq` 初始化（加入群不可看历史）

### Phase 3: WebSocket 网关 (3-4天)

- WebSocket 服务器
- 连接管理 (UserMap)
- 消息协议编解码
- 单连接写串行化（`send chan` + `writeLoop`）
- 写超时（write deadline）
- 心跳保活（ping/pong + read deadline）
- 慢消费者处理（踢下线/丢弃策略）

### Phase 4: 消息系统 (3-4天)

- 消息 Seq 生成
- 发送消息幂等（`client_msg_id`）
- 单聊消息发送/接收
- 群聊消息发送/接收
- 消息拉取（按 `min_seq/max_seq` 截断，对齐 open-im 的可见边界：入群不可看历史、退群不可看新消息）
- 已读游标更新（`read_seq` 单调递增）

### Phase 5: 会话管理 (1-2天)

- 会话列表
- 已读状态
- 会话置顶/免打扰

## 6. 后续扩展方向

- 消息离线推送 (APNs/FCM)
- 分布式部署 (Redis Pub/Sub/消息队列替代内存 UserMap；群成员缓存使用“本地缓存 + 失效事件”)
- 群推送并发限流（worker pool + 最大并发）
- 消息已读回执
- 消息撤回
- 文件/图片上传 (OSS)
- 消息搜索 (Elasticsearch)
