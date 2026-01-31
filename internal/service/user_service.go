package service

import (
	"context"

	"github.com/mbeoliero/kit/log"
	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/mbeoliero/nexo/internal/repository"
	"github.com/mbeoliero/nexo/pkg/errcode"
)

// UserService handles user-related business logic
type UserService struct {
	userRepo *repository.UserRepo
}

// NewUserService creates a new UserService
func NewUserService(userRepo *repository.UserRepo) *UserService {
	return &UserService{
		userRepo: userRepo,
	}
}

// GetUserInfo gets user info by Id
func (s *UserService) GetUserInfo(ctx context.Context, userId string) (*entity.UserInfo, error) {
	user, err := s.userRepo.GetById(ctx, userId)
	if err != nil {
		log.CtxDebug(ctx, "get user failed: user_id=%s, error=%v", userId, err)
		return nil, errcode.ErrUserNotFound
	}
	return user.ToUserInfo(), nil
}

// GetUserInfos gets multiple users info by Ids
func (s *UserService) GetUserInfos(ctx context.Context, userIds []string) ([]*entity.UserInfo, error) {
	users, err := s.userRepo.GetByIds(ctx, userIds)
	if err != nil {
		log.CtxError(ctx, "get users failed: %v", err)
		return nil, errcode.ErrInternalServer
	}

	infos := make([]*entity.UserInfo, 0, len(users))
	for _, user := range users {
		infos = append(infos, user.ToUserInfo())
	}
	return infos, nil
}

// UpdateUserRequest represents user update request
type UpdateUserRequest struct {
	Nickname string `json:"nickname,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	Extra    string `json:"extra,omitempty"`
}

// UpdateUserInfo updates user info
func (s *UserService) UpdateUserInfo(ctx context.Context, userId string, req *UpdateUserRequest) (*entity.UserInfo, error) {
	// Check if user exists
	exists, err := s.userRepo.Exists(ctx, userId)
	if err != nil {
		log.CtxError(ctx, "check user exists failed: %v", err)
		return nil, errcode.ErrInternalServer
	}
	if !exists {
		return nil, errcode.ErrUserNotFound
	}

	// Build updates map
	updates := make(map[string]interface{})
	if req.Nickname != "" {
		updates["nickname"] = req.Nickname
	}
	if req.Avatar != "" {
		updates["avatar"] = req.Avatar
	}
	if req.Extra != "" {
		updates["extra"] = req.Extra
	}

	if len(updates) > 0 {
		if err := s.userRepo.Update(ctx, userId, updates); err != nil {
			log.CtxError(ctx, "update user failed: %v", err)
			return nil, errcode.ErrInternalServer
		}
	}

	// Return updated user info
	return s.GetUserInfo(ctx, userId)
}
