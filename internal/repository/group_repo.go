package repository

import (
	"context"
	"fmt"

	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/mbeoliero/nexo/pkg/constant"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GroupRepo is the repository for group operations
type GroupRepo struct {
	db  *gorm.DB
	rdb *redis.Client
}

// NewGroupRepo creates a new GroupRepo
func NewGroupRepo(db *gorm.DB, rdb *redis.Client) *GroupRepo {
	return &GroupRepo{db: db, rdb: rdb}
}

// Create creates a new group
func (r *GroupRepo) Create(ctx context.Context, group *entity.Group) error {
	now := entity.NowUnixMilli()
	group.CreatedAt = now
	group.UpdatedAt = now
	return r.db.WithContext(ctx).Create(group).Error
}

// GetById gets group by Id
func (r *GroupRepo) GetById(ctx context.Context, id string) (*entity.Group, error) {
	var group entity.Group
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&group).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// GetByIdWithTx gets group by Id with transaction
func (r *GroupRepo) GetByIdWithTx(ctx context.Context, tx *gorm.DB, id string) (*entity.Group, error) {
	var group entity.Group
	err := tx.WithContext(ctx).Where("id = ?", id).First(&group).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// Update updates group info
func (r *GroupRepo) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	updates["updated_at"] = entity.NowUnixMilli()
	return r.db.WithContext(ctx).Model(&entity.Group{}).Where("id = ?", id).Updates(updates).Error
}

// Dismiss dismisses a group
func (r *GroupRepo) Dismiss(ctx context.Context, id string) error {
	return r.Update(ctx, id, map[string]interface{}{"status": constant.GroupStatusDismissed})
}

// AddMember adds a member to group using ON DUPLICATE KEY UPDATE for rejoining
func (r *GroupRepo) AddMember(ctx context.Context, tx *gorm.DB, member *entity.GroupMember) error {
	now := entity.NowUnixMilli()
	member.CreatedAt = now
	member.UpdatedAt = now

	// Use ON DUPLICATE KEY UPDATE for handling rejoin scenario
	result := tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "group_id"}, {Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"status":         constant.GroupMemberStatusNormal,
			"joined_at":      member.JoinedAt,
			"join_seq":       member.JoinSeq,
			"role_level":     member.RoleLevel,
			"inviter_user_id": member.InviterUserId,
			"updated_at":     now,
		}),
	}).Create(member)

	if result.Error != nil {
		return result.Error
	}

	// Invalidate cache
	r.invalidateMemberCache(ctx, member.GroupId)
	return nil
}

// GetMember gets a group member
func (r *GroupRepo) GetMember(ctx context.Context, groupId, userId string) (*entity.GroupMember, error) {
	var member entity.GroupMember
	err := r.db.WithContext(ctx).
		Where("group_id = ? AND user_id = ?", groupId, userId).
		First(&member).Error
	if err != nil {
		return nil, err
	}
	return &member, nil
}

// GetMemberWithTx gets a group member with transaction
func (r *GroupRepo) GetMemberWithTx(ctx context.Context, tx *gorm.DB, groupId, userId string) (*entity.GroupMember, error) {
	var member entity.GroupMember
	err := tx.WithContext(ctx).
		Where("group_id = ? AND user_id = ?", groupId, userId).
		First(&member).Error
	if err != nil {
		return nil, err
	}
	return &member, nil
}

// GetActiveMembers gets all active members of a group
func (r *GroupRepo) GetActiveMembers(ctx context.Context, groupId string) ([]*entity.GroupMember, error) {
	var members []*entity.GroupMember
	err := r.db.WithContext(ctx).
		Where("group_id = ? AND status = ?", groupId, constant.GroupMemberStatusNormal).
		Find(&members).Error
	if err != nil {
		return nil, err
	}
	return members, nil
}

// GetActiveMemberUserIds gets user Ids of all active members
func (r *GroupRepo) GetActiveMemberUserIds(ctx context.Context, groupId string) ([]string, error) {
	var userIds []string
	err := r.db.WithContext(ctx).
		Model(&entity.GroupMember{}).
		Where("group_id = ? AND status = ?", groupId, constant.GroupMemberStatusNormal).
		Pluck("user_id", &userIds).Error
	if err != nil {
		return nil, err
	}
	return userIds, nil
}

// UpdateMemberStatus updates member status
func (r *GroupRepo) UpdateMemberStatus(ctx context.Context, tx *gorm.DB, groupId, userId string, status int32) error {
	err := tx.WithContext(ctx).
		Model(&entity.GroupMember{}).
		Where("group_id = ? AND user_id = ?", groupId, userId).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": entity.NowUnixMilli(),
		}).Error
	if err != nil {
		return err
	}

	// Invalidate cache
	r.invalidateMemberCache(ctx, groupId)
	return nil
}

// GetMemberCount gets the count of active members in a group
func (r *GroupRepo) GetMemberCount(ctx context.Context, groupId string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&entity.GroupMember{}).
		Where("group_id = ? AND status = ?", groupId, constant.GroupMemberStatusNormal).
		Count(&count).Error
	return count, err
}

// IsActiveMember checks if user is an active member of the group
func (r *GroupRepo) IsActiveMember(ctx context.Context, groupId, userId string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&entity.GroupMember{}).
		Where("group_id = ? AND user_id = ? AND status = ?", groupId, userId, constant.GroupMemberStatusNormal).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetUserGroups gets all groups that user is an active member of
func (r *GroupRepo) GetUserGroups(ctx context.Context, userId string) ([]*entity.Group, error) {
	var groups []*entity.Group
	err := r.db.WithContext(ctx).
		Joins("JOIN group_members ON group_members.group_id = groups.id").
		Where("group_members.user_id = ? AND group_members.status = ?", userId, constant.GroupMemberStatusNormal).
		Find(&groups).Error
	if err != nil {
		return nil, err
	}
	return groups, nil
}

// invalidateMemberCache invalidates the group members cache
func (r *GroupRepo) invalidateMemberCache(ctx context.Context, groupId string) {
	key := fmt.Sprintf(constant.RedisKeyGroupMembers(), groupId)
	r.rdb.Del(ctx, key)
}
