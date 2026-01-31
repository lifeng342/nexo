# Nexo IM 系统实现计划

基于架构设计方案文档和 open-im-server 代码实现简化版 IM 系统。

## 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| HTTP 框架 | Hertz | 字节开源高性能框架 |
| WebSocket | gorilla/websocket + hertz adaptor | 长连接网关 |
| ORM | GORM 泛型 (`gorm.G[T]()`) | 类型安全的数据库操作 |
| 日志 | github.com/mbeoliero/kit/log | 用户指定 |
| 缓存 | Redis (go-redis/v9) | Seq分配、在线状态 |
| 认证 | JWT (golang-jwt/v5) | Token 管理 |
| 配置 | Viper | YAML 配置加载 |

## 实现阶段

### Phase 1: 基础框架 (第1-2天)

**文件创建顺序:**

1. `go.mod` - 初始化模块和依赖
2. `config/config.yaml` - 配置文件
3. `pkg/constant/constant.go` - 常量定义
4. `pkg/errcode/errcode.go` - 错误码
5. `pkg/response/response.go` - 统一响应
6. `pkg/jwt/jwt.go` - JWT 工具
7. `internal/config/config.go` - 配置加载
8. `internal/middleware/auth.go` - JWT 中间件
9. `internal/middleware/cors.go` - CORS 中间件

**关键依赖版本:**
```go
github.com/cloudwego/hertz v0.9.0
gorm.io/gorm v1.31.1
gorm.io/driver/mysql v1.5.7
github.com/redis/go-redis/v9 v9.5.1
github.com/golang-jwt/jwt/v5 v5.2.1
github.com/mbeoliero/kit latest
github.com/gorilla/websocket v1.5.3
github.com/spf13/viper v1.18.2
```

### Phase 2: 数据层 (第3-4天)

**创建 SQL Migration:**
- `migrations/001_init_schema.sql` - 7张表定义

**Entity 定义 (基于 entity_define.txt):**
- `internal/entity/base.go` - 公共方法
- `internal/entity/user.go` - User 实体
- `internal/entity/group.go` - Group, GroupMember 实体
- `internal/entity/message.go` - Message, MessageContent 实体
- `internal/entity/conversation.go` - Conversation 实体
- `internal/entity/seq.go` - SeqConversation, SeqUser 实体

**Repository 层 (使用 GORM 泛型):**
```go
// 泛型 Repository 示例
func (r *UserRepo) GetByID(ctx context.Context, id string) (*entity.User, error) {
    return gorm.G[entity.User](r.db).Where("id = ?", id).First(ctx)
}
```

- `internal/repository/base_repo.go` - 泛型基础 CRUD
- `internal/repository/user_repo.go`
- `internal/repository/group_repo.go` - 含缓存失效
- `internal/repository/message_repo.go` - 含幂等查询
- `internal/repository/conversation_repo.go`
- `internal/repository/seq_repo.go` - Redis INCR 分配 seq

### Phase 3: 业务层 (第5-7天)

**Service 层:**
- `internal/service/auth_service.go` - 登录/Token
- `internal/service/user_service.go` - 用户 CRUD
- `internal/service/group_service.go` - 群组管理、入群 join_seq
- `internal/service/message_service.go` - 消息发送/拉取
- `internal/service/conversation_service.go` - 会话管理

**Handler 层 (Hertz):**
- `internal/handler/auth_handler.go`
- `internal/handler/user_handler.go`
- `internal/handler/group_handler.go`
- `internal/handler/message_handler.go`
- `internal/handler/conversation_handler.go`

**关键业务逻辑:**

1. **消息幂等**: `(sender_id, client_msg_id)` 唯一约束
2. **Seq 分配**: Redis INCR → 定期同步 MySQL
3. **入群不可看历史**: `join_seq = max_seq + 1`, 写入 `seq_users.min_seq`
4. **退群不可看新消息**: `seq_users.max_seq = current_max_seq`
5. **群消息权限**: 发送前校验 `group_members.status = 0`

### Phase 4: WebSocket 网关 (第8-10天)

**Gateway 组件 (参考 open-im-server/internal/msggateway):**
- `internal/gateway/constant.go` - WS 协议常量
- `internal/gateway/protocol.go` - WSRequest/WSResponse
- `internal/gateway/client_conn.go` - 单写通道 + writeLoop
- `internal/gateway/client.go` - 客户端连接抽象
- `internal/gateway/user_map.go` - 多端连接映射
- `internal/gateway/ws_server.go` - WS 服务器
- `internal/gateway/message_handler.go` - 消息处理

**核心设计 (来自 open-im-server):**

```go
// 单写通道模式 - 防止并发写
type websocketClientConn struct {
    conn   *websocket.Conn
    writer chan []byte  // 缓冲写通道
}

func (c *websocketClientConn) writeLoop() {
    for msg := range c.writer {
        c.conn.SetWriteDeadline(time.Now().Add(WriteWait))
        c.conn.WriteMessage(websocket.BinaryMessage, msg)
    }
}
```

**异步推送:**
```go
type WsServer struct {
    pushChan chan *PushTask  // 10000 缓冲
}

func (s *WsServer) AsyncPush(data []byte, targetIds []string) {
    select {
    case s.pushChan <- &PushTask{...}:
    default:
        // 队列满，记录日志，客户端拉取补齐
    }
}
```

### Phase 5: 路由集成与启动 (第11天)

- `internal/router/router.go` - 路由注册
- `cmd/server/main.go` - 程序入口

**路由结构:**
```
/auth/login          POST  - 登录
/auth/register       POST  - 注册
/user/info           GET   - 获取用户信息
/user/update         PUT   - 更新用户信息
/group/create        POST  - 创建群组
/group/join          POST  - 加入群组
/group/quit          POST  - 退出群组
/group/info          GET   - 群组信息
/group/members       GET   - 群成员列表
/msg/send            POST  - 发送消息 (HTTP备用)
/msg/pull            GET   - 拉取消息
/conversation/list   GET   - 会话列表
/conversation/update PUT   - 更新会话设置
/ws                  GET   - WebSocket 连接
```

## 关键实现细节

### 1. GORM 泛型用法

```go
// 查询
user, err := gorm.G[entity.User](db).Where("id = ?", id).First(ctx)

// 批量查询
users, err := gorm.G[entity.User](db).Where("id IN ?", ids).Find(ctx)

// 创建
err := gorm.G[entity.User](db).Create(ctx, &user)

// 更新
err := gorm.G[entity.User](db).Where("id = ?", id).Update(ctx, "nickname", name)

// 删除
err := gorm.G[entity.User](db).Where("id = ?", id).Delete(ctx)
```

### 2. kit/log 集成

```go
import "github.com/mbeoliero/kit/log"

// 结构化日志
log.Info(ctx, "message sent", "conv_id", convId, "seq", seq)
log.Error(ctx, "send failed", "error", err)
log.Debug(ctx, "ws connection", "user_id", userId)
```

### 3. ConversationId 生成

```go
// 单聊: si_{min(userA,userB)}_{max(userA,userB)}
func GenSingleConversationId(userA, userB string) string {
    users := []string{userA, userB}
    sort.Strings(users)
    return fmt.Sprintf("si_%s_%s", users[0], users[1])
}

// 群聊: sg_{groupId}
func GenGroupConversationId(groupId string) string {
    return fmt.Sprintf("sg_%s", groupId)
}
```

### 4. WebSocket 连接地址

```
ws://host:port/ws?token={jwt}&send_id={user_id}&platform_id={1-5}&operation_id={uuid}
```

## 验证策略

### Phase 1 验证
- [ ] `go mod tidy` 成功
- [ ] 配置文件正确加载
- [ ] JWT Token 生成/解析正常

### Phase 2 验证
- [ ] SQL 迁移执行成功
- [ ] GORM 泛型 CRUD 测试通过
- [ ] Redis 连接正常

### Phase 3 验证
- [ ] 单聊消息发送/接收
- [ ] 群聊消息发送 (含权限校验)
- [ ] 消息幂等 (重复 client_msg_id 返回相同消息)
- [ ] 入群后只能看到新消息

### Phase 4 验证
- [ ] WebSocket 连接升级成功
- [ ] Token 校验拒绝非法连接
- [ ] 心跳保活 (ping/pong)
- [ ] 消息推送到在线用户

### Phase 5 验证 (端到端)
- [ ] 完整流程: 登录 → WS连接 → 发消息 → 接收推送
- [ ] 群组流程: 创建 → 加入 → 发消息 → 成员收到

## 关键文件路径

**参考文件 (open-im-server):**
- `/Users/bytedance/projects/nexo_v2/open-im-server/internal/msggateway/client.go` - Client 实现
- `/Users/bytedance/projects/nexo_v2/open-im-server/internal/msggateway/ws_server.go` - WS 服务器
- `/Users/bytedance/projects/nexo_v2/open-im-server/internal/msggateway/user_map.go` - 用户连接映射

**输入文件:**
- `/Users/bytedance/projects/nexo_v2/Nexo IM 系统架构设计方案.md` - 架构设计
- `/Users/bytedance/projects/nexo_v2/entity_define.txt` - 实体定义

**输出目录:**
- `/Users/bytedance/projects/nexo_v2/` - 项目根目录 (直接在此实现)
