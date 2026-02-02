# Nexo IM

Nexo IM 是一个使用 Go 语言构建的现代即时通讯系统后端服务，支持单聊、群聊和实时消息推送。

## 功能特性

- **用户认证**: JWT 认证，支持多平台登录（iOS、Android、Windows、macOS、Web）
- **单聊**: 一对一私聊消息
- **群聊**: 群组创建、加入、退出，角色权限管理
- **实时通讯**: WebSocket 实时消息推送
- **消息管理**: 支持文本、图片、视频、音频、文件等多种消息类型
- **会话管理**: 会话列表、未读消息计数、已读回执
- **消息幂等**: 基于 client_msg_id 的消息去重机制
- **序列号追踪**: 全局和用户级别的消息序列号，保证消息顺序

## 技术栈

- **语言**: Go 1.25
- **HTTP 框架**: [Cloudwego Hertz](https://github.com/cloudwego/hertz)
- **WebSocket**: [Gorilla WebSocket](https://github.com/gorilla/websocket)
- **ORM**: [GORM](https://gorm.io/)
- **数据库**: MySQL 8.0
- **缓存**: Redis 7.0
- **认证**: JWT
- **配置管理**: Viper

## 项目结构

```
nexo_v2/
├── cmd/
│   └── server/
│       └── main.go                 # 应用入口
├── internal/
│   ├── config/                     # 配置管理
│   ├── entity/                     # 数据模型
│   ├── gateway/                    # WebSocket 网关
│   ├── handler/                    # HTTP 处理器
│   ├── middleware/                 # 中间件（认证、CORS）
│   ├── repository/                 # 数据访问层
│   ├── router/                     # 路由定义
│   └── service/                    # 业务逻辑层
├── pkg/
│   ├── constant/                   # 常量定义
│   ├── errcode/                    # 错误码
│   ├── idgen/                      # ID 生成器
│   ├── jwt/                        # JWT 工具
│   └── response/                   # 响应格式化
├── migrations/                     # 数据库迁移脚本
├── tests/                          # 集成测试
└── config/                         # 配置文件
```

## 快速开始

### 环境要求

- Go 1.25+
- MySQL 8.0+
- Redis 7.0+

### 安装步骤

1. 克隆项目

```bash
git clone https://github.com/your-repo/nexo_v2.git
cd nexo_v2
```

2. 安装依赖

```bash
go mod download
```

3. 配置数据库

创建 MySQL 数据库并执行迁移脚本：

```bash
mysql -u root -p < migrations/001_init_schema.sql
```

4. 修改配置

编辑 `config/config.yaml`，配置数据库和 Redis 连接信息。

5. 启动服务

```bash
go run cmd/server/main.go
```

### Docker 部署

```bash
docker-compose up -d
```

## 配置说明

```yaml
server:
  http_port: 8080           # HTTP 服务端口
  ws_port: 8080             # WebSocket 端口
  mode: debug               # 运行模式 (debug/release)
  allowed_origins: ["*"]    # CORS 允许的源

mysql:
  host: localhost
  port: 3306
  user: root
  password: root
  database: nexo_im
  max_open_conns: 100
  max_idle_conns: 10

redis:
  host: localhost
  port: 6379
  password: ""
  db: 0
  key_prefix: "nexo:"

jwt:
  secret: "your-secret-key"
  expire_hours: 168         # Token 过期时间（小时）

websocket:
  max_conn_num: 10000       # 最大连接数
  max_message_size: 51200   # 最大消息大小（字节）
  push_worker_num: 10       # 推送工作协程数
```

## API 接口

### 认证

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/auth/register` | 用户注册 |
| POST | `/auth/login` | 用户登录 |

### 用户

| 方法 | 路径 | 描述 |
|------|------|------|
| GET | `/user/info` | 获取当前用户信息 |
| GET | `/user/profile/:user_id` | 获取其他用户资料 |
| PUT | `/user/update` | 更新用户信息 |

### 群组

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/group/create` | 创建群组 |
| POST | `/group/join` | 加入群组 |
| POST | `/group/quit` | 退出群组 |
| GET | `/group/info` | 获取群组信息 |
| GET | `/group/members` | 获取群成员列表 |

### 消息

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/msg/send` | 发送消息 |
| GET | `/msg/pull` | 拉取消息 |
| GET | `/msg/max_seq` | 获取最大序列号 |

### 会话

| 方法 | 路径 | 描述 |
|------|------|------|
| GET | `/conversation/list` | 获取会话列表 |
| GET | `/conversation/info` | 获取会话详情 |
| PUT | `/conversation/update` | 更新会话设置 |
| POST | `/conversation/mark_read` | 标记已读 |
| GET | `/conversation/unread_count` | 获取未读数 |

### WebSocket

| 路径 | 描述 |
|------|------|
| `/ws?token=xxx` | WebSocket 连接 |

## 数据模型

### 消息类型

| 类型 | 值 | 描述 |
|------|-----|------|
| Text | 1 | 文本消息 |
| Image | 2 | 图片消息 |
| Video | 3 | 视频消息 |
| Audio | 4 | 音频消息 |
| File | 5 | 文件消息 |
| Custom | 100 | 自定义消息 |

### 会话类型

| 类型 | 值 | 描述 |
|------|-----|------|
| Single | 1 | 单聊 |
| Group | 2 | 群聊 |

### 群成员角色

| 角色 | 值 | 描述 |
|------|-----|------|
| Member | 1 | 普通成员 |
| Admin | 2 | 管理员 |
| Owner | 3 | 群主 |

## 测试

运行集成测试：

```bash
go test ./tests/... -v
```

测试覆盖：
- 用户认证流程
- 单聊消息收发
- 群聊消息收发
- WebSocket 实时推送
- 会话管理
- 消息幂等性

## 架构设计

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Client    │────▶│   Handler   │────▶│   Service   │
└─────────────┘     └─────────────┘     └─────────────┘
                           │                    │
                           ▼                    ▼
                    ┌─────────────┐     ┌─────────────┐
                    │  Middleware │     │ Repository  │
                    └─────────────┘     └─────────────┘
                                               │
                           ┌───────────────────┼───────────────────┐
                           ▼                   ▼                   ▼
                    ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
                    │    MySQL    │     │    Redis    │     │  WebSocket  │
                    └─────────────┘     └─────────────┘     │   Gateway   │
                                                            └─────────────┘
```

## License

MIT License
