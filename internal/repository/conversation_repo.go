package repository

import (
	"context"
	"errors"

	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ConversationRepo is the repository for conversation operations
type ConversationRepo struct {
	db  *gorm.DB
	rdb *redis.Client
}

// NewConversationRepo creates a new ConversationRepo
func NewConversationRepo(db *gorm.DB, rdb *redis.Client) *ConversationRepo {
	return &ConversationRepo{db: db, rdb: rdb}
}

// Create creates a new conversation
func (r *ConversationRepo) Create(ctx context.Context, conv *entity.Conversation) error {
	now := entity.NowUnixMilli()
	conv.CreatedAt = now
	conv.UpdatedAt = now
	return r.db.WithContext(ctx).Create(conv).Error
}

// Upsert creates or updates a conversation
func (r *ConversationRepo) Upsert(ctx context.Context, conv *entity.Conversation) error {
	now := entity.NowUnixMilli()
	conv.UpdatedAt = now
	if conv.CreatedAt == 0 {
		conv.CreatedAt = now
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "owner_id"}, {Name: "conversation_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"updated_at": now,
		}),
	}).Create(conv).Error
}

// GetByOwnerAndConvId gets conversation by owner and conversation Id
func (r *ConversationRepo) GetByOwnerAndConvId(ctx context.Context, ownerId, conversationId string) (*entity.Conversation, error) {
	var conv entity.Conversation
	err := r.db.WithContext(ctx).
		Where("owner_id = ? AND conversation_id = ?", ownerId, conversationId).
		First(&conv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &conv, nil
}

// GetUserConversations gets all conversations for a user
func (r *ConversationRepo) GetUserConversations(ctx context.Context, ownerId string) ([]*entity.Conversation, error) {
	var convs []*entity.Conversation
	err := r.db.WithContext(ctx).
		Where("owner_id = ?", ownerId).
		Order("updated_at DESC").
		Find(&convs).Error
	if err != nil {
		return nil, err
	}
	return convs, nil
}

// GetUserConversationsWithSeq gets conversations with sequence info
func (r *ConversationRepo) GetUserConversationsWithSeq(ctx context.Context, ownerId string) ([]*entity.ConversationWithSeq, error) {
	var results []*entity.ConversationWithSeq

	err := r.db.WithContext(ctx).
		Table("conversations c").
		Select(`
			c.*,
			COALESCE(sc.max_seq, 0) as max_seq,
			COALESCE(su.read_seq, 0) as read_seq,
			GREATEST(0, COALESCE(sc.max_seq, 0) - COALESCE(su.read_seq, 0)) as unread_count
		`).
		Joins("LEFT JOIN seq_conversations sc ON sc.conversation_id = c.conversation_id").
		Joins("LEFT JOIN seq_users su ON su.user_id = c.owner_id AND su.conversation_id = c.conversation_id").
		Where("c.owner_id = ?", ownerId).
		Order("c.updated_at DESC").
		Scan(&results).Error

	if err != nil {
		return nil, err
	}
	return results, nil
}

// Update updates conversation settings
func (r *ConversationRepo) Update(ctx context.Context, ownerId, conversationId string, updates map[string]interface{}) error {
	updates["updated_at"] = entity.NowUnixMilli()
	return r.db.WithContext(ctx).
		Model(&entity.Conversation{}).
		Where("owner_id = ? AND conversation_id = ?", ownerId, conversationId).
		Updates(updates).Error
}

// Touch updates the updated_at timestamp
func (r *ConversationRepo) Touch(ctx context.Context, ownerId, conversationId string) error {
	return r.Update(ctx, ownerId, conversationId, map[string]interface{}{})
}

// EnsureSingleChatConversations ensures conversations exist for both parties in a single chat
// Each party's conversation has the other party as peer_user_id
func (r *ConversationRepo) EnsureSingleChatConversations(ctx context.Context, tx *gorm.DB, conversationId string, senderId, recvId string) error {
	now := entity.NowUnixMilli()

	// Create conversation for sender (peer is receiver)
	senderConv := &entity.Conversation{
		ConversationId:   conversationId,
		OwnerId:          senderId,
		ConversationType: 1, // Single chat
		PeerUserId:       recvId,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "owner_id"}, {Name: "conversation_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"updated_at": now,
		}),
	}).Create(senderConv).Error; err != nil {
		return err
	}

	// Create conversation for receiver (peer is sender)
	recvConv := &entity.Conversation{
		ConversationId:   conversationId,
		OwnerId:          recvId,
		ConversationType: 1, // Single chat
		PeerUserId:       senderId,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	return tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "owner_id"}, {Name: "conversation_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"updated_at": now,
		}),
	}).Create(recvConv).Error
}

// EnsureConversationsExist ensures conversations exist for all participants
// For single chat: creates conversation for both users
// For group chat: creates conversation for the user
func (r *ConversationRepo) EnsureConversationsExist(ctx context.Context, tx *gorm.DB, conversationId string, convType int32, userIds []string, groupId, peerUserId string) error {
	now := entity.NowUnixMilli()

	for _, userId := range userIds {
		conv := &entity.Conversation{
			ConversationId:   conversationId,
			OwnerId:          userId,
			ConversationType: convType,
			GroupId:          groupId,
			PeerUserId:       peerUserId,
			CreatedAt:        now,
			UpdatedAt:        now,
		}

		// For single chat, set peer_user_id correctly for each party
		if convType == 1 && peerUserId == userId {
			// This shouldn't happen, but handle it
			continue
		}

		err := tx.WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "owner_id"}, {Name: "conversation_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"updated_at": now,
			}),
		}).Create(conv).Error

		if err != nil {
			return err
		}
	}

	return nil
}
