# Nexo IM API 接口文档

## 目录

- [通用说明](#通用说明)
- [认证接口](#认证接口)
- [用户接口](#用户接口)
- [群组接口](#群组接口)
- [消息接口](#消息接口)
- [会话接口](#会话接口)
- [WebSocket 接口](#websocket-接口)
- [错误码](#错误码)

---

## 通用说明

### 基础信息

- **Base URL**: `http://localhost:8080`
- **Content-Type**: `application/json`
- **认证方式**: JWT Token（通过 `Authorization` Header 传递）

### 请求头

需要认证的接口必须携带以下请求头：

```
Authorization: Bearer <token>
```

### 响应格式

所有接口统一返回以下格式：

```json
{
  "code": 0,
  "msg": "success",
  "data": {}
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| code | int | 状态码，0 表示成功 |
| msg | string | 状态信息 |
| data | object | 响应数据 |

---

## 认证接口

### 用户注册

注册新用户账号。

**请求**

```
POST /auth/register
```

**请求参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_id | string | 否 | 用户 ID，不填则自动生成 UUID |
| nickname | string | 是 | 用户昵称 |
| password | string | 是 | 密码 |
| avatar | string | 否 | 头像 URL |

**请求示例**

```json
{
  "user_id": "user001",
  "nickname": "张三",
  "password": "123456",
  "avatar": "https://example.com/avatar.png"
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "user_id": "user001",
    "nickname": "张三",
    "avatar": "https://example.com/avatar.png",
    "extra": "",
    "created_at": 1706688000000
  }
}
```

---

### 用户登录

用户登录获取 Token。

**请求**

```
POST /auth/login
```

**请求参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_id | string | 是 | 用户 ID |
| password | string | 是 | 密码 |
| platform_id | int | 是 | 平台 ID（见下表） |

**平台 ID 说明**

| 值 | 平台 |
|----|------|
| 1 | iOS |
| 2 | Android |
| 3 | Windows |
| 4 | macOS |
| 5 | Web |

**请求示例**

```json
{
  "user_id": "user001",
  "password": "123456",
  "platform_id": 5
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "user_info": {
      "user_id": "user001",
      "nickname": "张三",
      "avatar": "https://example.com/avatar.png",
      "extra": "",
      "created_at": 1706688000000
    }
  }
}
```

**说明**
- 同一平台只允许一个设备登录，新登录会踢掉该平台的其他 Token

---

## 用户接口

> 以下接口需要认证

### 获取当前用户信息

获取当前登录用户的信息。

**请求**

```
GET /user/info
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "user_id": "user001",
    "nickname": "张三",
    "avatar": "https://example.com/avatar.png",
    "extra": "",
    "created_at": 1706688000000
  }
}
```

---

### 获取指定用户信息

根据用户 ID 获取用户信息。

**请求**

```
GET /user/profile/:user_id
```

**路径参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| user_id | string | 是 | 目标用户 ID |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "user_id": "user002",
    "nickname": "李四",
    "avatar": "https://example.com/avatar2.png",
    "extra": "",
    "created_at": 1706688000000
  }
}
```

---

### 更新用户信息

更新当前用户的信息。

**请求**

```
PUT /user/update
```

**请求参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| nickname | string | 否 | 新昵称 |
| avatar | string | 否 | 新头像 URL |
| extra | string | 否 | 扩展信息（JSON 字符串） |

**请求示例**

```json
{
  "nickname": "张三丰",
  "avatar": "https://example.com/new-avatar.png"
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "user_id": "user001",
    "nickname": "张三丰",
    "avatar": "https://example.com/new-avatar.png",
    "extra": "",
    "created_at": 1706688000000
  }
}
```

---

## 群组接口

> 以下接口需要认证

### 创建群组

创建一个新群组，创建者自动成为群主。

**请求**

```
POST /group/create
```

**请求参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| name | string | 是 | 群组名称 |
| introduction | string | 否 | 群组简介 |
| avatar | string | 否 | 群组头像 URL |
| member_ids | string[] | 否 | 初始成员 ID 列表 |

**请求示例**

```json
{
  "name": "技术交流群",
  "introduction": "讨论技术问题",
  "avatar": "https://example.com/group-avatar.png",
  "member_ids": ["user002", "user003"]
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": "1234567890",
    "name": "技术交流群",
    "introduction": "讨论技术问题",
    "avatar": "https://example.com/group-avatar.png",
    "status": 1,
    "creator_user_id": "user001",
    "created_at": 1706688000000,
    "updated_at": 1706688000000
  }
}
```

---

### 加入群组

加入一个已存在的群组。

**请求**

```
POST /group/join
```

**请求参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| group_id | string | 是 | 群组 ID |
| inviter_id | string | 否 | 邀请人 ID |

**请求示例**

```json
{
  "group_id": "1234567890",
  "inviter_id": "user001"
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": null
}
```

**说明**
- 新成员加入后只能看到加入时刻之后的消息
- 已是群成员会返回错误

---

### 退出群组

退出一个群组。

**请求**

```
POST /group/quit
```

**请求参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| group_id | string | 是 | 群组 ID |

**请求示例**

```json
{
  "group_id": "1234567890"
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": null
}
```

**说明**
- 群主不能退出群组，需要先转让群主或解散群组
- 退出后无法看到新消息，但可以看到退出前的历史消息

---

### 获取群组信息

获取群组详细信息。

**请求**

```
GET /group/info?group_id=xxx
```

**查询参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| group_id | string | 是 | 群组 ID |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": "1234567890",
    "name": "技术交流群",
    "introduction": "讨论技术问题",
    "avatar": "https://example.com/group-avatar.png",
    "status": 1,
    "creator_user_id": "user001",
    "member_count": 10,
    "created_at": 1706688000000
  }
}
```

**群组状态说明**

| 值 | 状态 |
|----|------|
| 1 | 正常 |
| 2 | 已解散 |

---

### 获取群成员列表

获取群组的所有活跃成员。

**请求**

```
GET /group/members?group_id=xxx
```

**查询参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| group_id | string | 是 | 群组 ID |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": [
    {
      "id": 1,
      "group_id": "1234567890",
      "user_id": "user001",
      "group_nickname": "",
      "role_level": 3,
      "status": 1,
      "joined_at": 1706688000000,
      "inviter_user_id": ""
    },
    {
      "id": 2,
      "group_id": "1234567890",
      "user_id": "user002",
      "group_nickname": "",
      "role_level": 1,
      "status": 1,
      "joined_at": 1706688000000,
      "inviter_user_id": "user001"
    }
  ]
}
```

**角色等级说明**

| 值 | 角色 |
|----|------|
| 1 | 普通成员 |
| 2 | 管理员 |
| 3 | 群主 |

**成员状态说明**

| 值 | 状态 |
|----|------|
| 1 | 正常 |
| 2 | 已退出 |
| 3 | 被踢出 |

---

## 消息接口

> 以下接口需要认证

### 发送消息

发送单聊或群聊消息。

**请求**

```
POST /msg/send
```

**请求参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| client_msg_id | string | 是 | 客户端消息 ID（用于幂等） |
| recv_id | string | 条件 | 接收者 ID（单聊必填） |
| group_id | string | 条件 | 群组 ID（群聊必填） |
| session_type | int | 是 | 会话类型：1-单聊，2-群聊 |
| msg_type | int | 是 | 消息类型（见下表） |
| content | object | 是 | 消息内容（见下方说明） |

**消息类型说明**

| 值 | 类型 | 说明 |
|----|------|------|
| 1 | Text | 文本消息 |
| 2 | Image | 图片消息 |
| 3 | Video | 视频消息 |
| 4 | Audio | 音频消息 |
| 5 | File | 文件消息 |
| 100 | Custom | 自定义消息 |

**消息内容格式**

文本消息：
```json
{
  "text": "消息内容"
}
```

图片消息：
```json
{
  "image_url": "https://example.com/image.png",
  "image_width": 800,
  "image_height": 600,
  "image_size": 102400
}
```

视频消息：
```json
{
  "video_url": "https://example.com/video.mp4",
  "video_duration": 120,
  "video_size": 10240000,
  "thumbnail_url": "https://example.com/thumb.png"
}
```

音频消息：
```json
{
  "audio_url": "https://example.com/audio.mp3",
  "audio_duration": 60,
  "audio_size": 1024000
}
```

文件消息：
```json
{
  "file_url": "https://example.com/file.pdf",
  "file_name": "document.pdf",
  "file_size": 2048000
}
```

自定义消息：
```json
{
  "custom_type": "location",
  "custom_data": "{\"lat\":39.9,\"lng\":116.4}"
}
```

**单聊请求示例**

```json
{
  "client_msg_id": "msg_uuid_001",
  "recv_id": "user002",
  "session_type": 1,
  "msg_type": 1,
  "content": {
    "text": "你好！"
  }
}
```

**群聊请求示例**

```json
{
  "client_msg_id": "msg_uuid_002",
  "group_id": "1234567890",
  "session_type": 2,
  "msg_type": 1,
  "content": {
    "text": "大家好！"
  }
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "conversation_id": "si_user001:user002",
    "seq": 1,
    "client_msg_id": "msg_uuid_001",
    "sender_id": "user001",
    "recv_id": "user002",
    "group_id": "",
    "session_type": 1,
    "msg_type": 1,
    "content": {
      "text": "你好！"
    },
    "send_at": 1706688000000
  }
}
```

**说明**
- `client_msg_id` 用于消息幂等，相同的 `client_msg_id` 只会发送一次
- 发送成功后会通过 WebSocket 推送给接收方

---

### 拉取消息

拉取指定会话的历史消息。

**请求**

```
GET /msg/pull?conversation_id=xxx&begin_seq=1&end_seq=100&limit=50
```

**查询参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| conversation_id | string | 是 | 会话 ID |
| begin_seq | int64 | 否 | 起始序列号（默认 0） |
| end_seq | int64 | 否 | 结束序列号（默认最大值） |
| limit | int | 否 | 返回数量限制（默认 100，最大 100） |

**会话 ID 格式**

| 类型 | 格式 | 示例 |
|------|------|------|
| 单聊 | `si_{userA}:{userB}` | `si_user001:user002` |
| 群聊 | `sg_{groupId}` | `sg_1234567890` |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "messages": [
      {
        "conversation_id": "si_user001:user002",
        "seq": 1,
        "client_msg_id": "msg_uuid_001",
        "sender_id": "user001",
        "recv_id": "user002",
        "group_id": "",
        "session_type": 1,
        "msg_type": 1,
        "content_text": "你好！",
        "send_at": 1706688000000
      }
    ],
    "max_seq": 10
  }
}
```

**说明**
- 用户只能拉取自己有权限访问的会话消息
- 群成员只能看到加入群组后的消息
- 退出群组后只能看到退出前的消息

---

### 获取最大序列号

获取指定会话的最大消息序列号。

**请求**

```
GET /msg/max_seq?conversation_id=xxx
```

**查询参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| conversation_id | string | 是 | 会话 ID |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "max_seq": 100
  }
}
```

---

## 会话接口

> 以下接口需要认证

### 获取会话列表

获取当前用户的所有会话。

**请求**

```
GET /conversation/list
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": [
    {
      "conversation_id": "si_user001:user002",
      "conversation_type": 1,
      "peer_user_id": "user002",
      "group_id": "",
      "recv_msg_opt": 0,
      "is_pinned": false,
      "unread_count": 5,
      "max_seq": 100,
      "read_seq": 95,
      "updated_at": 1706688000000
    },
    {
      "conversation_id": "sg_1234567890",
      "conversation_type": 2,
      "peer_user_id": "",
      "group_id": "1234567890",
      "recv_msg_opt": 0,
      "is_pinned": true,
      "unread_count": 10,
      "max_seq": 200,
      "read_seq": 190,
      "updated_at": 1706688000000
    }
  ]
}
```

**会话类型说明**

| 值 | 类型 |
|----|------|
| 1 | 单聊 |
| 2 | 群聊 |

**消息接收选项说明**

| 值 | 说明 |
|----|------|
| 0 | 正常接收 |
| 1 | 不提醒 |
| 2 | 不接收 |

---

### 获取会话详情

获取指定会话的详细信息。

**请求**

```
GET /conversation/info?conversation_id=xxx
```

**查询参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| conversation_id | string | 是 | 会话 ID |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "conversation_id": "si_user001:user002",
    "conversation_type": 1,
    "peer_user_id": "user002",
    "group_id": "",
    "recv_msg_opt": 0,
    "is_pinned": false,
    "unread_count": 5,
    "max_seq": 100,
    "read_seq": 95,
    "updated_at": 1706688000000
  }
}
```

---

### 更新会话设置

更新会话的设置（置顶、消息接收选项等）。

**请求**

```
PUT /conversation/update?conversation_id=xxx
```

**查询参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| conversation_id | string | 是 | 会话 ID |

**请求参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| recv_msg_opt | int | 否 | 消息接收选项 |
| is_pinned | bool | 否 | 是否置顶 |

**请求示例**

```json
{
  "is_pinned": true,
  "recv_msg_opt": 1
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": null
}
```

---

### 标记已读

标记会话消息为已读。

**请求**

```
POST /conversation/mark_read
```

**请求参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| conversation_id | string | 是 | 会话 ID |
| read_seq | int64 | 是 | 已读到的序列号 |

**请求示例**

```json
{
  "conversation_id": "si_user001:user002",
  "read_seq": 100
}
```

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": null
}
```

---

### 获取已读序列号

获取会话的最大序列号和已读序列号。

**请求**

```
GET /conversation/max_read_seq?conversation_id=xxx
```

**查询参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| conversation_id | string | 是 | 会话 ID |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "max_seq": 100,
    "read_seq": 95,
    "unread_count": 5
  }
}
```

---

### 获取未读数

获取会话的未读消息数量。

**请求**

```
GET /conversation/unread_count?conversation_id=xxx
```

**查询参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| conversation_id | string | 是 | 会话 ID |
| read_seq | int64 | 否 | 指定已读序列号（不填则使用当前已读序列号） |

**响应示例**

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "unread_count": 5
  }
}
```

---

## WebSocket 接口

### 建立连接

通过 WebSocket 建立实时消息连接。

**请求**

```
GET /ws?token=xxx
```

**查询参数**

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| token | string | 是 | JWT Token |

**连接示例**

```javascript
const ws = new WebSocket('ws://localhost:8080/ws?token=eyJhbGciOiJIUzI1NiIs...');

ws.onopen = function() {
  console.log('Connected');
};

ws.onmessage = function(event) {
  const message = JSON.parse(event.data);
  console.log('Received:', message);
};

ws.onclose = function() {
  console.log('Disconnected');
};
```

### 消息推送格式

服务端推送的消息格式：

```json
{
  "conversation_id": "si_user001:user002",
  "seq": 1,
  "client_msg_id": "msg_uuid_001",
  "sender_id": "user001",
  "recv_id": "user002",
  "group_id": "",
  "session_type": 1,
  "msg_type": 1,
  "content": {
    "text": "你好！"
  },
  "send_at": 1706688000000
}
```

---

## 错误码

### 通用错误 (1xxx)

| 错误码 | 说明 |
|--------|------|
| 0 | 成功 |
| 1001 | 参数无效 |
| 1002 | 服务器内部错误 |
| 1003 | 未授权 |
| 1004 | 禁止访问 |
| 1005 | 资源不存在 |
| 1006 | 请求过于频繁 |
| 1007 | 无权限访问该资源 |

### 认证错误 (2xxx)

| 错误码 | 说明 |
|--------|------|
| 2001 | Token 无效 |
| 2002 | Token 已过期 |
| 2003 | Token 缺失 |
| 2004 | Token 用户不匹配 |
| 2005 | 登录失败 |
| 2006 | 用户不存在 |
| 2007 | 用户已存在 |
| 2008 | 密码错误 |

### 群组错误 (3xxx)

| 错误码 | 说明 |
|--------|------|
| 3001 | 群组不存在 |
| 3002 | 群组已解散 |
| 3003 | 不是群成员 |
| 3004 | 成员状态异常 |
| 3005 | 已是群成员 |
| 3006 | 不是群主 |
| 3007 | 不是管理员 |
| 3008 | 无法踢出群主 |

### 消息错误 (4xxx)

| 错误码 | 说明 |
|--------|------|
| 4001 | 消息不存在 |
| 4002 | 重复消息 |
| 4003 | 会话不存在 |
| 4004 | 序列号分配失败 |
| 4005 | 消息发送失败 |
| 4006 | 消息拉取失败 |

### WebSocket 错误 (5xxx)

| 错误码 | 说明 |
|--------|------|
| 5001 | 连接数超过限制 |
| 5002 | 连接已关闭 |
| 5003 | 协议无效 |
| 5004 | 消息推送失败 |

---

## 健康检查

### 服务健康检查

检查服务是否正常运行。

**请求**

```
GET /health
```

**响应示例**

```json
{
  "status": "ok"
}
```
