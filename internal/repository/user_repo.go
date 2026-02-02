package repository

import (
	"context"

	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// UserRepo is the repository for user operations
type UserRepo struct {
	db  *gorm.DB
	rdb *redis.Client
}

// NewUserRepo creates a new UserRepo
func NewUserRepo(db *gorm.DB, rdb *redis.Client) *UserRepo {
	return &UserRepo{db: db, rdb: rdb}
}

// Create creates a new user
func (r *UserRepo) Create(ctx context.Context, user *entity.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

// GetById gets user by Id
func (r *UserRepo) GetById(ctx context.Context, id string) (*entity.User, error) {
	var user entity.User
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetByIds gets users by Ids
func (r *UserRepo) GetByIds(ctx context.Context, ids []string) ([]*entity.User, error) {
	var users []*entity.User
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}

// Update updates user info
func (r *UserRepo) Update(ctx context.Context, id string, updates map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&entity.User{}).Where("id = ?", id).Updates(updates).Error
}

// Exists checks if user exists
func (r *UserRepo) Exists(ctx context.Context, id string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&entity.User{}).Where("id = ?", id).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetByIdWithTx gets user by Id with transaction
func (r *UserRepo) GetByIdWithTx(ctx context.Context, tx *gorm.DB, id string) (*entity.User, error) {
	var user entity.User
	err := tx.WithContext(ctx).Where("id = ?", id).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}
