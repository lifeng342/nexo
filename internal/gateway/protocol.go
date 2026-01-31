package gateway

import "encoding/json"

// WSRequest represents a WebSocket request message
type WSRequest struct {
	ReqIdentifier int32  `json:"req_identifier"` // Request type
	MsgIncr       string `json:"msg_incr"`       // Client message counter/trace Id
	OperationId   string `json:"operation_id"`   // Operation Id
	Token         string `json:"token"`          // JWT token (optional, used in handshake)
	SendId        string `json:"send_id"`        // Sender user Id
	Data          []byte `json:"data"`           // Business data
}

// WSResponse represents a WebSocket response message
type WSResponse struct {
	ReqIdentifier int32  `json:"req_identifier"` // Request type (echo back)
	MsgIncr       string `json:"msg_incr"`       // Message counter (echo back)
	OperationId   string `json:"operation_id"`   // Operation Id (echo back)
	ErrCode       int    `json:"err_code"`       // Error code, 0 = success
	ErrMsg        string `json:"err_msg"`        // Error message
	Data          []byte `json:"data"`           // Response data
}

// SendMsgReq represents send message request data
type SendMsgReq struct {
	ClientMsgId string `json:"client_msg_id"`
	RecvId      string `json:"recv_id,omitempty"`
	GroupId     string `json:"group_id,omitempty"`
	SessionType int32  `json:"session_type"`
	MsgType     int32  `json:"msg_type"`
	Content     struct {
		Text   string `json:"text,omitempty"`
		Image  string `json:"image,omitempty"`
		Video  string `json:"video,omitempty"`
		Audio  string `json:"audio,omitempty"`
		File   string `json:"file,omitempty"`
		Custom string `json:"custom,omitempty"`
	} `json:"content"`
}

// SendMsgResp represents send message response data
type SendMsgResp struct {
	ServerMsgId    int64  `json:"server_msg_id"`
	ConversationId string `json:"conversation_id"`
	Seq            int64  `json:"seq"`
	ClientMsgId    string `json:"client_msg_id"`
	SendAt         int64  `json:"send_at"`
}

// PullMsgReq represents pull messages request data
type PullMsgReq struct {
	ConversationId string  `json:"conversation_id"`
	BeginSeq       int64   `json:"begin_seq"`
	EndSeq         int64   `json:"end_seq"`
	Limit          int     `json:"limit"`
	SeqList        []int64 `json:"seq_list,omitempty"` // For WSPullMsgBySeqList
}

// PullMsgResp represents pull messages response data
type PullMsgResp struct {
	Messages []*MessageData `json:"messages"`
	MaxSeq   int64          `json:"max_seq"`
}

// MessageData represents message data in response
type MessageData struct {
	ServerMsgId    int64  `json:"server_msg_id"`
	ConversationId string `json:"conversation_id"`
	Seq            int64  `json:"seq"`
	ClientMsgId    string `json:"client_msg_id"`
	SenderId       string `json:"sender_id"`
	RecvId         string `json:"recv_id,omitempty"`
	GroupId        string `json:"group_id,omitempty"`
	SessionType    int32  `json:"session_type"`
	MsgType        int32  `json:"msg_type"`
	Content        struct {
		Text   string `json:"text,omitempty"`
		Image  string `json:"image,omitempty"`
		Video  string `json:"video,omitempty"`
		Audio  string `json:"audio,omitempty"`
		File   string `json:"file,omitempty"`
		Custom string `json:"custom,omitempty"`
	} `json:"content"`
	SendAt int64 `json:"send_at"`
}

// GetNewestSeqReq represents get newest seq request
type GetNewestSeqReq struct {
	ConversationIds []string `json:"conversation_ids"`
}

// GetNewestSeqResp represents get newest seq response
type GetNewestSeqResp struct {
	Seqs map[string]int64 `json:"seqs"` // conversation_id -> max_seq
}

// GetConvMaxReadSeqReq represents get conversation max/read seq request
type GetConvMaxReadSeqReq struct {
	ConversationId string `json:"conversation_id"`
}

// GetConvMaxReadSeqResp represents get conversation max/read seq response
type GetConvMaxReadSeqResp struct {
	MaxSeq      int64 `json:"max_seq"`
	ReadSeq     int64 `json:"read_seq"`
	UnreadCount int64 `json:"unread_count"`
}

// PushMsgData represents push message data
type PushMsgData struct {
	Msgs map[string][]*MessageData `json:"msgs"` // conversation_id -> messages
}

// Encode encodes data to JSON bytes
func Encode(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Decode decodes JSON bytes to struct
func Decode(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
