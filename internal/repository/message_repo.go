package repository

import (
	"context"
	"errors"

	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// MessageRepo is the repository for message operations
type MessageRepo struct {
	db  *gorm.DB
	rdb *redis.Client
}

// NewMessageRepo creates a new MessageRepo
func NewMessageRepo(db *gorm.DB, rdb *redis.Client) *MessageRepo {
	return &MessageRepo{db: db, rdb: rdb}
}

// Create creates a new message
func (r *MessageRepo) Create(ctx context.Context, tx *gorm.DB, msg *entity.Message) error {
	now := entity.NowUnixMilli()
	msg.CreatedAt = now
	msg.UpdatedAt = now
	return tx.WithContext(ctx).Create(msg).Error
}

// GetByClientMsgId gets message by sender_id and client_msg_id (for idempotency check)
func (r *MessageRepo) GetByClientMsgId(ctx context.Context, senderId, clientMsgId string) (*entity.Message, error) {
	var msg entity.Message
	err := r.db.WithContext(ctx).
		Where("sender_id = ? AND client_msg_id = ?", senderId, clientMsgId).
		First(&msg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &msg, nil
}

// GetByConvSeq gets message by conversation_id and seq
func (r *MessageRepo) GetByConvSeq(ctx context.Context, conversationId string, seq int64) (*entity.Message, error) {
	var msg entity.Message
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND seq = ?", conversationId, seq).
		First(&msg).Error
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

// PullMessages pulls messages in a conversation within seq range
// limit is capped at 100
func (r *MessageRepo) PullMessages(ctx context.Context, conversationId string, beginSeq, endSeq int64, limit int) ([]*entity.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}

	var messages []*entity.Message
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND seq >= ? AND seq <= ?", conversationId, beginSeq, endSeq).
		Order("seq ASC").
		Limit(limit).
		Find(&messages).Error
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// PullMessagesBySeqList pulls messages by specific seq list
func (r *MessageRepo) PullMessagesBySeqList(ctx context.Context, conversationId string, seqList []int64) ([]*entity.Message, error) {
	if len(seqList) == 0 {
		return nil, nil
	}

	// Cap at 100
	if len(seqList) > 100 {
		seqList = seqList[:100]
	}

	var messages []*entity.Message
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND seq IN ?", conversationId, seqList).
		Order("seq ASC").
		Find(&messages).Error
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// GetLatestMessages gets the latest N messages in a conversation
func (r *MessageRepo) GetLatestMessages(ctx context.Context, conversationId string, limit int) ([]*entity.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var messages []*entity.Message
	err := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationId).
		Order("seq DESC").
		Limit(limit).
		Find(&messages).Error
	if err != nil {
		return nil, err
	}

	// Reverse to ascending order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetMessageCountAfterSeq gets count of messages after a specific seq
func (r *MessageRepo) GetMessageCountAfterSeq(ctx context.Context, conversationId string, seq int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&entity.Message{}).
		Where("conversation_id = ? AND seq > ?", conversationId, seq).
		Count(&count).Error
	return count, err
}
