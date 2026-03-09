package gateway

import "time"

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

func nowMillis() int64 { return time.Now().UnixMilli() }
