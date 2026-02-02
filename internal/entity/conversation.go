package entity

// Conversation represents a conversation
type Conversation struct {
	Id               int64   `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	ConversationId   string  `json:"conversation_id" gorm:"column:conversation_id"`
	OwnerId          string  `json:"owner_id" gorm:"column:owner_id"`
	ConversationType int32   `json:"conversation_type" gorm:"column:conversation_type"`
	PeerUserId       string  `json:"peer_user_id" gorm:"column:peer_user_id"`
	GroupId          string  `json:"group_id" gorm:"column:group_id"`
	RecvMsgOpt       int32   `json:"recv_msg_opt" gorm:"column:recv_msg_opt"`
	IsPinned         bool    `json:"is_pinned" gorm:"column:is_pinned"`
	Extra            *string `json:"extra" gorm:"column:extra;type:json"`
	CreatedAt        int64   `json:"created_at" gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt        int64   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime:milli"`
}

// TableName returns the table name for Conversation
func (Conversation) TableName() string {
	return "conversations"
}

// ConversationInfo represents conversation info for API response
type ConversationInfo struct {
	ConversationId   string `json:"conversation_id"`
	ConversationType int32  `json:"conversation_type"`
	PeerUserId       string `json:"peer_user_id,omitempty"`
	GroupId          string `json:"group_id,omitempty"`
	RecvMsgOpt       int32  `json:"recv_msg_opt"`
	IsPinned         bool   `json:"is_pinned"`
	UnreadCount      int64  `json:"unread_count"`
	MaxSeq           int64  `json:"max_seq"`
	ReadSeq          int64  `json:"read_seq"`
	UpdatedAt        int64  `json:"updated_at"`
}

// ConversationWithSeq represents conversation with seq info
type ConversationWithSeq struct {
	Conversation
	MaxSeq      int64 `json:"max_seq"`
	ReadSeq     int64 `json:"read_seq"`
	UnreadCount int64 `json:"unread_count"`
}
