# Nexo IM SDK

Nexo IM 系统的 Go SDK，供后端服务调用 IM 系统的 API。

## 安装

```bash
go get github.com/mbeoliero/nexo/sdk
```

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/mbeoliero/nexo/sdk"
)

func main() {
    ctx := context.Background()

    // 创建客户端 (使用 Hertz HTTP client)
    client := sdk.MustNewClient("http://localhost:8080")

    // 用户注册
    userInfo, err := client.Register(ctx, &sdk.RegisterRequest{
        UserId:   "user123",
        Nickname: "John",
        Password: "password123",
    })
    if err != nil {
        log.Fatalf("register failed: %v", err)
    }
    fmt.Printf("Registered user: %s\n", userInfo.Nickname)

    // 用户登录 (登录成功后 token 会自动保存)
    loginResp, err := client.Login(ctx, &sdk.LoginRequest{
        UserId:     "user123",
        Password:   "password123",
        PlatformId: sdk.PlatformIdWeb,
    })
    if err != nil {
        log.Fatalf("login failed: %v", err)
    }
    fmt.Printf("Logged in: %s, token: %s\n", loginResp.UserInfo.Nickname, loginResp.Token)

    // 获取用户信息
    info, err := client.GetUserInfo(ctx)
    if err != nil {
        log.Fatalf("get user info failed: %v", err)
    }
    fmt.Printf("User info: %+v\n", info)
}
```

## API 文档

### 认证 (Auth)

```go
// 注册
userInfo, err := client.Register(ctx, &sdk.RegisterRequest{
    UserId:   "user123",
    Nickname: "John",
    Password: "password123",
    Avatar:   "https://example.com/avatar.png", // 可选
})

// 登录
loginResp, err := client.Login(ctx, &sdk.LoginRequest{
    UserId:     "user123",
    Password:   "password123",
    PlatformId: sdk.PlatformIdWeb, // 1=iOS, 2=Android, 3=Windows, 4=macOS, 5=Web
})

// 快捷登录
loginResp, err := client.LoginWithUserId(ctx, "user123", "password123", sdk.PlatformIdWeb)
```

### 用户 (User)

```go
// 获取当前用户信息
userInfo, err := client.GetUserInfo(ctx)

// 获取指定用户信息
userInfo, err := client.GetUserInfoById(ctx, "user456")

// 更新用户信息
userInfo, err := client.UpdateUserInfo(ctx, &sdk.UpdateUserRequest{
    Nickname: "New Name",
    Avatar:   "https://example.com/new-avatar.png",
})

// 获取用户在线状态
statuses, err := client.GetUsersOnlineStatus(ctx, []string{"user1", "user2", "user3"})
```

### 群组 (Group)

```go
// 创建群组
groupInfo, err := client.CreateGroup(ctx, &sdk.CreateGroupRequest{
    Name:         "My Group",
    Introduction: "Group description",
    MemberIds:    []string{"user2", "user3"}, // 初始成员
})

// 加入群组
err := client.JoinGroup(ctx, "group123", "inviter_user_id")

// 退出群组
err := client.QuitGroup(ctx, "group123")

// 获取群组信息
groupInfo, err := client.GetGroupInfo(ctx, "group123")

// 获取群成员列表
members, err := client.GetGroupMembers(ctx, "group123")
```

### 消息 (Message)

```go
// 发送单聊文本消息
msg, err := client.SendTextMessage(ctx, "client-msg-id-1", "receiver_user_id", "Hello!")

// 发送群聊文本消息
msg, err := client.SendGroupTextMessage(ctx, "client-msg-id-2", "group123", "Hello group!")

// 发送自定义消息
msg, err := client.SendMessage(ctx, &sdk.SendMessageRequest{
    ClientMsgId: "client-msg-id-3",
    RecvId:      "receiver_user_id", // 单聊
    // GroupId:  "group123",        // 群聊时使用
    SessionType: sdk.SessionTypeSingle,
    MsgType:     sdk.MsgTypeImage,
    Content: sdk.MessageContent{
        Image: "https://example.com/image.png",
    },
})

// 拉取消息
resp, err := client.PullMessages(ctx, "conversation_id", 0, 0, 50)
// resp.Messages - 消息列表
// resp.MaxSeq - 会话最大序列号

// 获取会话最大序列号
maxSeq, err := client.GetMaxSeq(ctx, "conversation_id")
```

### 会话 (Conversation)

```go
// 获取会话列表
conversations, err := client.GetConversationList(ctx)

// 获取指定会话
conversation, err := client.GetConversation(ctx, "conversation_id")

// 设置会话置顶
err := client.SetConversationPinned(ctx, "conversation_id", true)

// 设置免打扰
err := client.SetConversationRecvMsgOpt(ctx, "conversation_id", sdk.RecvMsgOptNoNotify)

// 标记已读
err := client.MarkRead(ctx, "conversation_id", 100) // 已读到 seq=100

// 获取已读/未读信息
seqInfo, err := client.GetMaxReadSeq(ctx, "conversation_id")
// seqInfo.MaxSeq - 最大序列号
// seqInfo.ReadSeq - 已读序列号  
// seqInfo.UnreadCount - 未读数

// 获取未读数
unreadCount, err := client.GetUnreadCount(ctx, "conversation_id", 0)
```

## 常量

### 会话类型 (SessionType)
- `SessionTypeSingle = 1` - 单聊
- `SessionTypeGroup = 2` - 群聊

### 消息类型 (MsgType)
- `MsgTypeText = 1` - 文本
- `MsgTypeImage = 2` - 图片
- `MsgTypeVideo = 3` - 视频
- `MsgTypeAudio = 4` - 语音
- `MsgTypeFile = 5` - 文件
- `MsgTypeCustom = 100` - 自定义

### 平台 Id (PlatformId)
- `PlatformIdIOS = 1`
- `PlatformIdAndroid = 2`
- `PlatformIdWindows = 3`
- `PlatformIdMacOS = 4`
- `PlatformIdWeb = 5`

### 消息接收选项 (RecvMsgOpt)
- `RecvMsgOptNormal = 0` - 正常接收
- `RecvMsgOptNoNotify = 1` - 接收但不提醒
- `RecvMsgOptNotRecv = 2` - 不接收

## 错误处理

SDK 返回的错误可以转换为 `*sdk.Error` 类型以获取错误码：

```go
msg, err := client.SendTextMessage(ctx, "msg-id", "user456", "Hello")
if err != nil {
    if sdkErr, ok := err.(*sdk.Error); ok {
        fmt.Printf("API error: code=%d, msg=%s\n", sdkErr.Code, sdkErr.Msg)
        
        // 根据错误码处理
        switch sdkErr.Code {
        case sdk.CodeUnauthorized:
            // Token 无效，需要重新登录
        case sdk.CodeUserNotFound:
            // 用户不存在
        default:
            // 其他错误
        }
    } else {
        // 网络错误或其他错误
        fmt.Printf("Error: %v\n", err)
    }
}
```

## 自定义配置

```go
import (
    "github.com/cloudwego/hertz/pkg/app/client"
)

// 自定义 Hertz HTTP Client
httpClient, _ := client.NewClient(
    client.WithDialTimeout(60*time.Second),
)

client, err := sdk.NewClient(
    "http://localhost:8080",
    sdk.WithHertzClient(httpClient),
    sdk.WithToken("existing-token"), // 使用已有 token
)

// 或使用 MustNewClient (出错时 panic)
client := sdk.MustNewClient("http://localhost:8080")
```

## License

MIT
