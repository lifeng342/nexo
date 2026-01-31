package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/mbeoliero/nexo/pkg/constant"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SeqRepo is the repository for sequence operations
type SeqRepo struct {
	db  *gorm.DB
	rdb *redis.Client
}

// NewSeqRepo creates a new SeqRepo
func NewSeqRepo(db *gorm.DB, rdb *redis.Client) *SeqRepo {
	return &SeqRepo{db: db, rdb: rdb}
}

// AllocSeq allocates a new sequence number for a conversation using Redis INCR
func (r *SeqRepo) AllocSeq(ctx context.Context, conversationId string) (int64, error) {
	key := fmt.Sprintf(constant.RedisKeySeqConversation(), conversationId)
	seq, err := r.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	return seq, nil
}

// GetMaxSeq gets the current max sequence for a conversation
func (r *SeqRepo) GetMaxSeq(ctx context.Context, conversationId string) (int64, error) {
	// Try Redis first
	key := fmt.Sprintf(constant.RedisKeySeqConversation(), conversationId)
	seq, err := r.rdb.Get(ctx, key).Int64()
	if err == nil {
		return seq, nil
	}
	if !errors.Is(err, redis.Nil) {
		return 0, err
	}

	// Fall back to MySQL
	var seqConv entity.SeqConversation
	err = r.db.WithContext(ctx).Where("conversation_id = ?", conversationId).First(&seqConv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}

	// Restore to Redis
	r.rdb.Set(ctx, key, seqConv.MaxSeq, 0)

	return seqConv.MaxSeq, nil
}

// GetMaxSeqWithLock gets max seq with row lock for joining group
func (r *SeqRepo) GetMaxSeqWithLock(ctx context.Context, tx *gorm.DB, conversationId string) (int64, error) {
	var seqConv entity.SeqConversation
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("conversation_id = ?", conversationId).
		First(&seqConv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return seqConv.MaxSeq, nil
}

// SyncSeqToMySQL syncs the Redis sequence to MySQL
func (r *SeqRepo) SyncSeqToMySQL(ctx context.Context, conversationId string, maxSeq int64) error {
	seqConv := &entity.SeqConversation{
		ConversationId: conversationId,
		MaxSeq:         maxSeq,
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "conversation_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"max_seq"}),
	}).Create(seqConv).Error
}

// SyncSeqToMySQLWithTx syncs the Redis sequence to MySQL within a transaction
func (r *SeqRepo) SyncSeqToMySQLWithTx(ctx context.Context, tx *gorm.DB, conversationId string, maxSeq int64) error {
	seqConv := &entity.SeqConversation{
		ConversationId: conversationId,
		MaxSeq:         maxSeq,
	}

	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "conversation_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"max_seq"}),
	}).Create(seqConv).Error
}

// InitSeqFromMySQL initializes Redis seq from MySQL on startup
func (r *SeqRepo) InitSeqFromMySQL(ctx context.Context, conversationId string) error {
	var seqConv entity.SeqConversation
	err := r.db.WithContext(ctx).Where("conversation_id = ?", conversationId).First(&seqConv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	key := fmt.Sprintf(constant.RedisKeySeqConversation(), conversationId)
	return r.rdb.Set(ctx, key, seqConv.MaxSeq, 0).Err()
}

// GetSeqUser gets user sequence info for a conversation
func (r *SeqRepo) GetSeqUser(ctx context.Context, userId, conversationId string) (*entity.SeqUser, error) {
	var seqUser entity.SeqUser
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND conversation_id = ?", userId, conversationId).
		First(&seqUser).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &seqUser, nil
}

// UpsertSeqUser creates or updates user sequence info
func (r *SeqRepo) UpsertSeqUser(ctx context.Context, tx *gorm.DB, seqUser *entity.SeqUser) error {
	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "conversation_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"min_seq":  gorm.Expr("GREATEST(min_seq, ?)", seqUser.MinSeq),
			"max_seq":  gorm.Expr("GREATEST(max_seq, ?)", seqUser.MaxSeq),
			"read_seq": gorm.Expr("GREATEST(read_seq, ?)", seqUser.ReadSeq),
		}),
	}).Create(seqUser).Error
}

// SetSeqUserMinSeq sets the min_seq for a user in a conversation (used when joining group)
// Also resets max_seq to 0 to allow seeing new messages after rejoining
func (r *SeqRepo) SetSeqUserMinSeq(ctx context.Context, tx *gorm.DB, userId, conversationId string, minSeq int64) error {
	seqUser := &entity.SeqUser{
		UserId:         userId,
		ConversationId: conversationId,
		MinSeq:         minSeq,
		MaxSeq:         0,            // No upper limit
		ReadSeq:        minSeq - 1,   // Set read_seq to just before min_seq
	}

	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "conversation_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"min_seq":  minSeq,
			"max_seq":  0,            // Reset max_seq to allow seeing new messages
			"read_seq": minSeq - 1,
		}),
	}).Create(seqUser).Error
}

// SetSeqUserMaxSeq sets the max_seq for a user in a conversation (used when leaving group)
func (r *SeqRepo) SetSeqUserMaxSeq(ctx context.Context, tx *gorm.DB, userId, conversationId string, maxSeq int64) error {
	return tx.WithContext(ctx).
		Model(&entity.SeqUser{}).
		Where("user_id = ? AND conversation_id = ?", userId, conversationId).
		Update("max_seq", maxSeq).Error
}

// UpdateReadSeq updates the read_seq for a user in a conversation
// Uses upsert to create record if it doesn't exist
func (r *SeqRepo) UpdateReadSeq(ctx context.Context, userId, conversationId string, readSeq int64) error {
	seqUser := &entity.SeqUser{
		UserId:         userId,
		ConversationId: conversationId,
		ReadSeq:        readSeq,
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "conversation_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"read_seq": gorm.Expr("GREATEST(read_seq, ?)", readSeq),
		}),
	}).Create(seqUser).Error
}

// GetConversationSeqInfo gets sequence info for a conversation
func (r *SeqRepo) GetConversationSeqInfo(ctx context.Context, conversationId string) (*entity.SeqConversation, error) {
	var seqConv entity.SeqConversation
	err := r.db.WithContext(ctx).Where("conversation_id = ?", conversationId).First(&seqConv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &entity.SeqConversation{ConversationId: conversationId}, nil
		}
		return nil, err
	}
	return &seqConv, nil
}

// EnsureSeqConversationExists ensures seq_conversations record exists
func (r *SeqRepo) EnsureSeqConversationExists(ctx context.Context, tx *gorm.DB, conversationId string) error {
	seqConv := &entity.SeqConversation{
		ConversationId: conversationId,
		MaxSeq:         0,
		MinSeq:         0,
	}

	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "conversation_id"}},
		DoNothing: true,
	}).Create(seqConv).Error
}
