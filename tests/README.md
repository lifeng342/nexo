# Nexo IM 集成测试

本目录包含 Nexo IM 系统的自动化集成测试。

## 测试结构

```
tests/
├── api_test.go           # 测试框架和公共工具
├── auth_test.go          # 认证相关测试（注册、登录、Token）
├── user_test.go          # 用户相关测试（获取信息、更新）
├── group_test.go         # 群组相关测试（创建、加入、退出）
├── message_test.go       # 消息相关测试（发送、拉取、幂等性）
├── conversation_test.go  # 会话相关测试（列表、未读、已读）
├── websocket_test.go     # WebSocket 测试（连接、实时消息）
└── flow_test.go          # 完整流程测试（端到端场景）
```

## 前置条件

1. **启动依赖服务**

```bash
# MySQL
docker run -d --name mysql -p 3306:3306 \
  -e MYSQL_ROOT_PASSWORD=root \
  -e MYSQL_DATABASE=nexo_im \
  mysql:8

# Redis
docker run -d --name redis -p 6379:6379 redis:7
```

2. **初始化数据库**

```bash
mysql -h localhost -u root -proot nexo_im < migrations/001_init_schema.sql
```

3. **启动服务器**

```bash
go run cmd/server/main.go
```

## 运行测试

### 运行所有测试

```bash
cd tests
go test -v ./...
```

### 运行特定模块测试

```bash
# 认证测试
go test -v -run TestAuth ./...

# 消息测试
go test -v -run TestMessage ./...

# 群组测试
go test -v -run TestGroup ./...

# 会话测试
go test -v -run TestConversation ./...

# WebSocket 测试
go test -v -run TestWebSocket ./...

# 完整流程测试
go test -v -run TestFullFlow ./...
```

### 运行单个测试

```bash
go test -v -run TestAuth_Register ./...
go test -v -run TestMessage_SendSingleChat ./...
```

### 指定服务器地址

```bash
TEST_BASE_URL=http://192.168.1.100:8080 go test -v ./...
```

## 测试覆盖的功能

### 认证模块 (auth_test.go)
- [x] 用户注册
- [x] 重复注册检测
- [x] 用户登录
- [x] 密码错误处理
- [x] 用户不存在处理
- [x] 多平台登录
- [x] Token 验证

### 用户模块 (user_test.go)
- [x] 获取当前用户信息
- [x] 获取其他用户信息
- [x] 更新用户昵称
- [x] 更新用户头像

### 群组模块 (group_test.go)
- [x] 创建群组
- [x] 创建群组并添加初始成员
- [x] 加入群组
- [x] 重复加入检测
- [x] 退出群组
- [x] 群主不能退出
- [x] 获取群组信息
- [x] 获取群组成员

### 消息模块 (message_test.go)
- [x] 发送单聊消息
- [x] 发送群聊消息
- [x] 消息幂等性（client_msg_id）
- [x] 非群成员不能发送
- [x] 拉取消息
- [x] 消息分页
- [x] 消息权限控制
- [x] 获取最大序列号

### 会话模块 (conversation_test.go)
- [x] 获取会话列表
- [x] 获取会话信息
- [x] 标记已读
- [x] 获取未读数
- [x] 群聊会话

### WebSocket 模块 (websocket_test.go)
- [x] WebSocket 连接
- [x] 无效 Token 连接失败
- [x] 实时接收消息
- [x] 多连接支持
- [x] 群消息推送

### 完整流程 (flow_test.go)
- [x] 单聊完整流程
- [x] 群聊完整流程
- [x] 新成员消息可见性

## 错误码参考

| 错误码 | 说明 |
|--------|------|
| 0 | 成功 |
| 1001 | 参数错误 |
| 1003 | 未授权 |
| 1007 | 无权限 |
| 2006 | 用户不存在 |
| 2007 | 用户已存在 |
| 2008 | 密码错误 |
| 3001 | 群组不存在 |
| 3003 | 非群成员 |
| 3005 | 已是群成员 |
| 3008 | 不能踢出群主 |

## 添加新测试

1. 在对应的 `*_test.go` 文件中添加测试函数
2. 使用 `RegisterAndLogin` 辅助函数创建测试用户
3. 使用 `AssertSuccess` 和 `AssertError` 进行断言
4. 使用 `generateUserId` 生成唯一用户 ID 避免冲突

示例：

```go
func TestMyFeature(t *testing.T) {
    userId := generateUserId("my_test")
    client, _ := RegisterAndLogin(t, userId, "Test User", "password123")

    t.Run("test case name", func(t *testing.T) {
        resp, err := client.GET("/my/endpoint")
        if err != nil {
            t.Fatalf("request failed: %v", err)
        }
        AssertSuccess(t, resp, "should succeed")
    })
}
```
