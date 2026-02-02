# Open-IM-Server 消息序列化与压缩分析

## 各层序列化方式总结

| 层级 | 序列化方式 | 压缩 |
|-----|-----------|------|
| **客户端 ↔ WebSocket Gateway** | GOB (Go SDK) / JSON (JS SDK) | Gzip (可选) |
| **WebSocket Gateway ↔ RPC** | Protobuf | 无 |
| **RPC 层 (gRPC)** | Protobuf | 无 |
| **Kafka 消息队列** | Protobuf | 无 |
| **HTTP API 层** | JSON | 无 |

---

## 一、WebSocket 层（客户端对端层）

### 1.1 编码器实现

**文件**: `internal/msggateway/encoder.go`

```go
// Encoder 接口定义
type Encoder interface {
    Encode(data any) ([]byte, error)
    Decode(encodeData []byte, decodeData any) error
}

// GOB编码器 - 用于Go SDK
type GobEncoder struct{}

func (g *GobEncoder) Encode(data any) ([]byte, error) {
    buff := bytes.NewBuffer([]byte{})
    if err := gob.NewEncoder(buff).Encode(data); err != nil {
        return nil, err
    }
    return buff.Bytes(), nil
}

func (g *GobEncoder) Decode(encodeData []byte, decodeData any) error {
    return gob.NewDecoder(bytes.NewBuffer(encodeData)).Decode(decodeData)
}

// JSON编码器 - 用于JavaScript/其他SDK
type JsonEncoder struct{}

func (j *JsonEncoder) Encode(data any) ([]byte, error) {
    return json.Marshal(data)
}

func (j *JsonEncoder) Decode(encodeData []byte, decodeData any) error {
    return json.Unmarshal(encodeData, decodeData)
}
```

### 1.2 Gzip 压缩实现

**文件**: `internal/msggateway/compressor.go`

```go
// 使用 sync.Pool 优化性能，减少 GC 压力
var gzipWriterPool = sync.Pool{
    New: func() any { return gzip.NewWriter(nil) },
}

var gzipReaderPool = sync.Pool{
    New: func() any { return new(gzip.Reader) },
}

// Compressor 接口
type Compressor interface {
    Compress(rawData []byte) ([]byte, error)
    CompressWithPool(rawData []byte) ([]byte, error)
    DeCompress(compressedData []byte) ([]byte, error)
    DecompressWithPool(compressedData []byte) ([]byte, error)
}

// GzipCompressor 实现
type GzipCompressor struct{}

func (g *GzipCompressor) CompressWithPool(rawData []byte) ([]byte, error) {
    gz := gzipWriterPool.Get().(*gzip.Writer)
    defer gzipWriterPool.Put(gz)

    var buf bytes.Buffer
    gz.Reset(&buf)

    if _, err := gz.Write(rawData); err != nil {
        return nil, err
    }
    if err := gz.Close(); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}

func (g *GzipCompressor) DecompressWithPool(compressedData []byte) ([]byte, error) {
    gz := gzipReaderPool.Get().(*gzip.Reader)
    defer gzipReaderPool.Put(gz)

    if err := gz.Reset(bytes.NewReader(compressedData)); err != nil {
        return nil, err
    }
    defer gz.Close()

    return io.ReadAll(gz)
}
```

### 1.3 WebSocket 客户端消息处理

**文件**: `internal/msggateway/client.go`

```go
type Client struct {
    // ...
    IsCompress bool    // 是否启用压缩
    Encoder    Encoder // 编码器实例 (GOB/JSON)
}

// 消息接收处理流程
func (c *Client) handleMessage(message []byte) error {
    // 1. 如果启用压缩，先解压
    if c.IsCompress {
        message, err = c.Compressor.DecompressWithPool(message)
    }

    // 2. 使用编码器解码
    req := Req{}
    err = c.Encoder.Decode(message, &req)

    // 3. 根据消息类型路由处理
    switch req.ReqIdentifier {
    case WSGetNewestSeq:
        // ...
    case WSSendMsg:
        // ...
    }
}

// 消息发送处理流程
func (c *Client) writeBinaryMsg(resp Resp) error {
    // 1. 使用编码器编码
    encodedBuf, err := c.Encoder.Encode(resp)

    // 2. 如果启用压缩，进行压缩
    if c.IsCompress {
        resultBuf, err := c.Compressor.CompressWithPool(encodedBuf)
        return c.conn.WriteMessage(resultBuf)
    }

    return c.conn.WriteMessage(encodedBuf)
}
```

### 1.4 压缩标志获取

**文件**: `internal/msggateway/context.go`

```go
func (c *ConnContext) GetCompression() bool {
    return c.Query(Compression) == GzipCompressionProtocol
}
```

**常量定义**: `internal/msggateway/constant.go`

```go
const (
    Compression            = "compression"
    GzipCompressionProtocol = "gzip"
)
```

---

## 二、RPC 层 (gRPC)

### 2.1 Message Handler

**文件**: `internal/msggateway/message_handler.go`

WebSocket Gateway 与后端 RPC 服务之间使用 Protobuf：

```go
type GrpcHandler struct {
    msgClient   *rpcli.MsgClient
    pushClient  *rpcli.PushMsgServiceClient
    userClient  *rpcli.UserClient
    // ...
}

// 获取最大序列号
func (g *GrpcHandler) GetSeq(ctx context.Context, data *Req) ([]byte, error) {
    // Protobuf 反序列化请求
    req := sdkws.GetMaxSeqReq{}
    if err := proto.Unmarshal(data.Data, &req); err != nil {
        return nil, err
    }

    // gRPC 调用
    resp, err := g.msgClient.MsgClient.GetMaxSeq(ctx, &req)
    if err != nil {
        return nil, err
    }

    // Protobuf 序列化响应
    return proto.Marshal(resp)
}

// 发送消息
func (g *GrpcHandler) SendMessage(ctx context.Context, data *Req) ([]byte, error) {
    // Protobuf 反序列化
    msgData := sdkws.MsgData{}
    if err := proto.Unmarshal(data.Data, &msgData); err != nil {
        return nil, err
    }

    // gRPC 调用
    resp, err := g.msgClient.MsgClient.SendMsg(ctx, &pbmsg.SendMsgReq{MsgData: &msgData})
    if err != nil {
        return nil, err
    }

    // Protobuf 序列化响应
    return proto.Marshal(resp)
}

// 拉取消息
func (g *GrpcHandler) PullMessageBySeqList(ctx context.Context, data *Req) ([]byte, error) {
    req := sdkws.PullMessageBySeqsReq{}
    if err := proto.Unmarshal(data.Data, &req); err != nil {
        return nil, err
    }

    resp, err := g.msgClient.MsgClient.PullMessageBySeqs(ctx, &req)
    if err != nil {
        return nil, err
    }

    return proto.Marshal(resp)
}
```

---

## 三、HTTP API 层

**文件**: `internal/api/msg.go`

HTTP API 层使用 JSON（与客户端通信）+ Protobuf（与内部服务通信）：

```go
func (m *MessageApi) SendMessage(c *gin.Context) {
    // 1. HTTP JSON 反序列化请求
    var req apistruct.SendMsgReq
    if err := c.BindJSON(&req); err != nil {
        apiresp.GinError(c, err)
        return
    }

    // 2. 构建 Protobuf 消息结构
    sendMsgReq := m.getSendMsgReq(c, req.SendMsg)

    // 3. gRPC 调用（使用 Protobuf）
    respPb, err := m.Client.SendMsg(c, sendMsgReq)
    if err != nil {
        apiresp.GinError(c, err)
        return
    }

    // 4. HTTP JSON 序列化响应
    m.ginRespSendMsg(c, sendMsgReq, respPb)
}

func (m *MessageApi) ginRespSendMsg(c *gin.Context, req *msg.SendMsgReq, resp *msg.SendMsgResp) {
    apiresp.GinSuccess(c, &apistruct.SendMsgResp{
        ServerMsgID: resp.ServerMsgID,
        ClientMsgID: resp.ClientMsgID,
        SendTime:    resp.SendTime,
    })
}
```

### 3.1 Protobuf 反射使用

```go
var (
    msgDataDescriptor       protoreflect.FieldDescriptors
    getMsgDataDescriptorOnce sync.Once
)

func getMsgDataDescriptor() protoreflect.FieldDescriptors {
    getMsgDataDescriptorOnce.Do(func() {
        msgDataDescriptor = new(sdkws.MsgData).ProtoReflect().Descriptor().Fields()
    })
    return msgDataDescriptor
}
```

---

## 四、Kafka 消息队列

**文件**: `pkg/common/storage/kafka/producer.go`

消息队列统一使用 Protobuf：

```go
func (p *Producer) SendMessage(ctx context.Context, key string, msg proto.Message) error {
    // Protobuf 序列化
    bMsg, err := proto.Marshal(msg)
    if err != nil {
        return err
    }

    // 构建 Kafka 消息
    kMsg := &sarama.ProducerMessage{
        Topic: p.topic,
        Key:   sarama.StringEncoder(key),
        Value: sarama.ByteEncoder(bMsg),
    }

    // 发送到 Kafka
    partition, offset, err := p.producer.SendMessage(kMsg)
    if err != nil {
        return err
    }

    log.ZDebug(ctx, "kafka send success", "topic", p.topic, "key", key,
        "partition", partition, "offset", offset)
    return nil
}
```

**消费端反序列化**: `internal/msgtransfer/online_msg_to_mongo_handler.go`

```go
func (mc *OnlineMsgToMongoHandler) HandleMessage(ctx context.Context, msg []byte) error {
    // Protobuf 反序列化
    msgFromMQ := pbmsg.MsgDataToMongoByMQ{}
    if err := proto.Unmarshal(msg, &msgFromMQ); err != nil {
        return err
    }

    // 存入 MongoDB
    return mc.msgTransferDatabase.BatchInsertChat2DB(ctx,
        msgFromMQ.ConversationID,
        msgFromMQ.MsgData,
        msgFromMQ.LastSeq)
}
```

---

## 五、完整数据流图

```
┌─────────────────────────────────────────────────────────────────┐
│  客户端 (WebSocket)                                              │
│  ┌─────────────┐    ┌─────────────┐                              │
│  │ Go SDK      │    │ JS SDK      │                              │
│  │ GOB 编码    │    │ JSON 编码   │                              │
│  └──────┬──────┘    └──────┬──────┘                              │
│         └────────┬─────────┘                                     │
│                  ↓                                               │
│         ┌────────────────┐                                       │
│         │ Gzip 压缩(可选) │                                       │
│         └────────┬───────┘                                       │
└──────────────────┼──────────────────────────────────────────────┘
                   ↓
┌──────────────────▼──────────────────────────────────────────────┐
│  WebSocket Gateway (msggateway)                                  │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │ 1. Gzip 解压 (DecompressWithPool)                           │ │
│  │ 2. GOB/JSON 解码 (Encoder.Decode)                           │ │
│  │ 3. 路由到 MessageHandler                                    │ │
│  │ 4. Protobuf 序列化 (proto.Unmarshal)                        │ │
│  └─────────────────────────────────────────────────────────────┘ │
└──────────────────┬──────────────────────────────────────────────┘
                   ↓ Protobuf (gRPC)
┌──────────────────▼──────────────────────────────────────────────┐
│  RPC 服务层 (rpc/msg, rpc/user, rpc/push)                        │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │ - proto.Unmarshal(request)                                  │ │
│  │ - 业务逻辑处理                                               │ │
│  │ - proto.Marshal(response)                                   │ │
│  └─────────────────────────────────────────────────────────────┘ │
└──────────────────┬──────────────────────────────────────────────┘
                   ↓ Protobuf
┌──────────────────▼──────────────────────────────────────────────┐
│  Kafka 消息队列                                                  │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │ Producer: proto.Marshal(msg) → Sarama                       │ │
│  │ Consumer: Sarama → proto.Unmarshal(msg)                     │ │
│  └─────────────────────────────────────────────────────────────┘ │
└──────────────────┬──────────────────────────────────────────────┘
                   ↓
┌──────────────────▼──────────────────────────────────────────────┐
│  MongoDB 存储                                                    │
│  BatchInsertChat2DB(conversationID, msgData, lastSeq)           │
└─────────────────────────────────────────────────────────────────┘

═══════════════════════════════════════════════════════════════════

HTTP API 层处理流程：

┌─────────────────────────────────────────────────────────────────┐
│  HTTP 客户端                                                     │
│  JSON 请求                                                       │
└──────────────────┬──────────────────────────────────────────────┘
                   ↓
┌──────────────────▼──────────────────────────────────────────────┐
│  API 层 (internal/api)                                           │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │ 1. c.BindJSON(&req) [JSON 反序列化]                         │ │
│  │ 2. 构建 Protobuf 结构                                       │ │
│  │ 3. gRPC 调用 (Protobuf)                                     │ │
│  │ 4. apiresp.GinSuccess(c, resp) [JSON 序列化响应]            │ │
│  └─────────────────────────────────────────────────────────────┘ │
└──────────────────┬──────────────────────────────────────────────┘
                   ↓ Protobuf (gRPC)
┌──────────────────▼──────────────────────────────────────────────┐
│  RPC 服务层                                                      │
└─────────────────────────────────────────────────────────────────┘
```

---

## 六、关键配置参数

**文件**: `internal/msggateway/constant.go`

| 常量 | 值 | 说明 |
|-----|-----|------|
| `Compression` | "compression" | 查询参数名 |
| `GzipCompressionProtocol` | "gzip" | 压缩协议值 |
| `maxMessageSize` | 51200 | 最大消息大小（字节） |
| `pongWait` | 30s | Pong 超时时间 |
| `pingPeriod` | 27s | Ping 间隔 |

---

## 七、性能优化点

### 7.1 sync.Pool 对象复用

**文件**: `internal/msggateway/compressor.go`

```go
var gzipWriterPool = sync.Pool{
    New: func() any { return gzip.NewWriter(nil) },
}

var gzipReaderPool = sync.Pool{
    New: func() any { return new(gzip.Reader) },
}
```

- 减少内存分配和 GC 压力
- 适用于高并发场景

### 7.2 请求对象池

**文件**: `internal/msggateway/message_handler.go`

```go
var reqPool = sync.Pool{
    New: func() any { return new(Req) },
}

func getReq() *Req {
    req := reqPool.Get().(*Req)
    req.ReqIdentifier = 0
    req.Token = ""
    req.SendID = ""
    req.OperationID = ""
    req.MsgIncr = ""
    req.Data = nil
    return req
}

func freeReq(req *Req) {
    reqPool.Put(req)
}
```

### 7.3 Protobuf 反射缓存

**文件**: `internal/api/msg.go`

```go
var (
    msgDataDescriptor        protoreflect.FieldDescriptors
    getMsgDataDescriptorOnce sync.Once
)

func getMsgDataDescriptor() protoreflect.FieldDescriptors {
    getMsgDataDescriptorOnce.Do(func() {
        msgDataDescriptor = new(sdkws.MsgData).ProtoReflect().Descriptor().Fields()
    })
    return msgDataDescriptor
}
```

---

## 八、设计总结

1. **分层序列化策略**:
   - 客户端层使用 GOB/JSON，兼容不同语言 SDK
   - 内部服务层统一使用 Protobuf，性能更高

2. **可选压缩**:
   - Gzip 压缩在 WebSocket 层可选启用
   - 通过查询参数 `compression=gzip` 控制

3. **对象复用**:
   - 使用 sync.Pool 复用 gzip reader/writer
   - 使用对象池复用请求结构体

4. **协议统一**:
   - RPC 层、Kafka 层、存储层统一使用 Protobuf
   - 便于维护和扩展
