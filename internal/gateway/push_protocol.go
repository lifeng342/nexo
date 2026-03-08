package gateway

import "github.com/mbeoliero/nexo/internal/entity"

// PushMode describes how remote payloads are delivered.
type PushMode string

const (
	PushModeRoute     PushMode = "route"
	PushModeBroadcast PushMode = "broadcast"
)

// RouteConn is the distributed route DTO.
type RouteConn struct {
	UserId     string
	ConnId     string
	InstanceId string
	PlatformId int
}

// PushContent is the payload content subset shared by local/remote push.
type PushContent struct {
	Text   string `json:"text,omitempty"`
	Image  string `json:"image,omitempty"`
	Video  string `json:"video,omitempty"`
	Audio  string `json:"audio,omitempty"`
	File   string `json:"file,omitempty"`
	Custom string `json:"custom,omitempty"`
}

// PushPayload is the business payload delivered to remote instances.
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

// ConnRef is the route envelope target reference.
type ConnRef struct {
	UserId     string `json:"user_id"`
	ConnId     string `json:"conn_id"`
	PlatformId int    `json:"platform_id"`
}

// PushEnvelope carries routing metadata and one payload instance.
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

func newPushPayload(msg *entity.Message) *PushPayload {
	custom := ""
	if msg.ContentCustom != nil {
		custom = *msg.ContentCustom
	}

	return &PushPayload{
		MsgId:          msg.Id,
		ConversationId: msg.ConversationId,
		Seq:            msg.Seq,
		ClientMsgId:    msg.ClientMsgId,
		SenderId:       msg.SenderId,
		RecvId:         msg.RecvId,
		GroupId:        msg.GroupId,
		SessionType:    msg.SessionType,
		MsgType:        msg.MsgType,
		Content: PushContent{
			Text:   msg.ContentText,
			Image:  msg.ContentImage,
			Video:  msg.ContentVideo,
			Audio:  msg.ContentAudio,
			File:   msg.ContentFile,
			Custom: custom,
		},
		SendAt: msg.SendAt,
	}
}
