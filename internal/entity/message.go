package entity

// MessageContent represents the content of a message
type MessageContent struct {
	Text   string `json:"text,omitempty"`
	Image  string `json:"image,omitempty"`
	Video  string `json:"video,omitempty"`
	Audio  string `json:"audio,omitempty"`
	File   string `json:"file,omitempty"`
	Custom string `json:"custom,omitempty"`
}

// Message represents a message
type Message struct {
	Id             int64   `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	ConversationId string  `json:"conversation_id" gorm:"column:conversation_id"`
	Seq            int64   `json:"seq" gorm:"column:seq"`
	ClientMsgId    string  `json:"client_msg_id" gorm:"column:client_msg_id"`
	SenderId       string  `json:"sender_id" gorm:"column:sender_id"`
	RecvId         string  `json:"recv_id" gorm:"column:recv_id"`
	GroupId        string  `json:"group_id" gorm:"column:group_id"`
	SessionType    int32   `json:"session_type" gorm:"column:session_type"`
	MsgType        int32   `json:"msg_type" gorm:"column:msg_type"`
	ContentText    string  `json:"content_text" gorm:"column:content_text"`
	ContentImage   string  `json:"content_image" gorm:"column:content_image"`
	ContentVideo   string  `json:"content_video" gorm:"column:content_video"`
	ContentAudio   string  `json:"content_audio" gorm:"column:content_audio"`
	ContentFile    string  `json:"content_file" gorm:"column:content_file"`
	ContentCustom  *string `json:"content_custom" gorm:"column:content_custom;type:json"`
	Extra          *string `json:"extra" gorm:"column:extra;type:json"`
	SendAt         int64   `json:"send_at" gorm:"column:send_at"`
	CreatedAt      int64   `json:"created_at" gorm:"column:created_at"`
	UpdatedAt      int64   `json:"updated_at" gorm:"column:updated_at"`
}

// TableName returns the table name for Message
func (Message) TableName() string {
	return "messages"
}

// GetContent returns message content as struct
func (m *Message) GetContent() MessageContent {
	custom := ""
	if m.ContentCustom != nil {
		custom = *m.ContentCustom
	}
	return MessageContent{
		Text:   m.ContentText,
		Image:  m.ContentImage,
		Video:  m.ContentVideo,
		Audio:  m.ContentAudio,
		File:   m.ContentFile,
		Custom: custom,
	}
}

// SetContent sets message content from struct
func (m *Message) SetContent(c MessageContent) {
	m.ContentText = c.Text
	m.ContentImage = c.Image
	m.ContentVideo = c.Video
	m.ContentAudio = c.Audio
	m.ContentFile = c.File
	if c.Custom != "" {
		m.ContentCustom = &c.Custom
	}
}

// MessageInfo represents message info for API response
type MessageInfo struct {
	Id             int64          `json:"id"`
	ConversationId string         `json:"conversation_id"`
	Seq            int64          `json:"seq"`
	ClientMsgId    string         `json:"client_msg_id"`
	SenderId       string         `json:"sender_id"`
	SessionType    int32          `json:"session_type"`
	MsgType        int32          `json:"msg_type"`
	Content        MessageContent `json:"content"`
	SendAt         int64          `json:"send_at"`
}

// ToMessageInfo converts Message to MessageInfo
func (m *Message) ToMessageInfo() *MessageInfo {
	return &MessageInfo{
		Id:             m.Id,
		ConversationId: m.ConversationId,
		Seq:            m.Seq,
		ClientMsgId:    m.ClientMsgId,
		SenderId:       m.SenderId,
		SessionType:    m.SessionType,
		MsgType:        m.MsgType,
		Content:        m.GetContent(),
		SendAt:         m.SendAt,
	}
}
