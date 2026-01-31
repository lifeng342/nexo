package entity

// SeqConversation represents conversation sequence info
type SeqConversation struct {
	ConversationId string `json:"conversation_id" gorm:"column:conversation_id;primaryKey"`
	MaxSeq         int64  `json:"max_seq" gorm:"column:max_seq"`
	MinSeq         int64  `json:"min_seq" gorm:"column:min_seq"`
}

// TableName returns the table name for SeqConversation
func (SeqConversation) TableName() string {
	return "seq_conversations"
}

// SeqUser represents user sequence info per conversation
type SeqUser struct {
	Id             int64  `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	UserId         string `json:"user_id" gorm:"column:user_id"`
	ConversationId string `json:"conversation_id" gorm:"column:conversation_id"`
	MinSeq         int64  `json:"min_seq" gorm:"column:min_seq"`
	MaxSeq         int64  `json:"max_seq" gorm:"column:max_seq"`
	ReadSeq        int64  `json:"read_seq" gorm:"column:read_seq"`
}

// TableName returns the table name for SeqUser
func (SeqUser) TableName() string {
	return "seq_users"
}

// SeqRange represents visible seq range for a user
type SeqRange struct {
	MinSeq  int64 `json:"min_seq"`
	MaxSeq  int64 `json:"max_seq"`
	ReadSeq int64 `json:"read_seq"`
}

// GetVisibleRange calculates the visible seq range for pulling messages
// convMaxSeq: the max seq of the conversation
// Returns: (actualMinSeq, actualMaxSeq)
func (s *SeqUser) GetVisibleRange(convMaxSeq int64) (int64, int64) {
	minSeq := s.MinSeq
	maxSeq := convMaxSeq

	// If user has max_seq set (left/kicked from group), use it as upper bound
	if s.MaxSeq > 0 && s.MaxSeq < maxSeq {
		maxSeq = s.MaxSeq
	}

	return minSeq, maxSeq
}

// ClampSeqRange clamps the requested seq range to visible range
func (s *SeqUser) ClampSeqRange(beginSeq, endSeq, convMaxSeq int64) (int64, int64) {
	minVisible, maxVisible := s.GetVisibleRange(convMaxSeq)

	// Clamp begin seq
	if beginSeq < minVisible {
		beginSeq = minVisible
	}

	// Clamp end seq
	if endSeq > maxVisible {
		endSeq = maxVisible
	}

	return beginSeq, endSeq
}
