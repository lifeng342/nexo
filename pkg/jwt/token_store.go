package jwt

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Token status constants
const (
	TokenStatusNormal  = 1 // Token is valid
	TokenStatusKicked  = 2 // Token was kicked by new login
	TokenStatusExpired = 3 // Token expired
	TokenStatusLogout  = 4 // Token was logged out
)

// TokenStore manages token storage in Redis
type TokenStore struct {
	rdb          *redis.Client
	accessExpire time.Duration
	keyPrefix    string
}

// NewTokenStore creates a new TokenStore
func NewTokenStore(rdb *redis.Client, expireHours int) *TokenStore {
	return &TokenStore{
		rdb:          rdb,
		accessExpire: time.Duration(expireHours) * time.Hour,
		keyPrefix:    "nexo:token:",
	}
}

// tokenKey generates Redis key for user's tokens on a platform
// Format: nexo:token:{userId}:{platformId}
func (s *TokenStore) tokenKey(userId string, platformId int) string {
	return fmt.Sprintf("%s%s:%d", s.keyPrefix, userId, platformId)
}

// StoreToken stores a token in Redis with status
func (s *TokenStore) StoreToken(ctx context.Context, userId string, platformId int, token string) error {
	key := s.tokenKey(userId, platformId)

	// Use hash to store multiple tokens per user/platform
	// Field: token, Value: status
	if err := s.rdb.HSet(ctx, key, token, TokenStatusNormal).Err(); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}

	// Set expiration on the key
	if err := s.rdb.Expire(ctx, key, s.accessExpire).Err(); err != nil {
		return fmt.Errorf("failed to set token expiration: %w", err)
	}

	return nil
}

// ValidateTokenStatus checks if a token exists and is valid in Redis
// Returns: status (0 if not found), error
func (s *TokenStore) ValidateTokenStatus(ctx context.Context, userId string, platformId int, token string) (int, error) {
	key := s.tokenKey(userId, platformId)

	statusStr, err := s.rdb.HGet(ctx, key, token).Result()
	if err == redis.Nil {
		// Token not found in Redis
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get token status: %w", err)
	}

	status, err := strconv.Atoi(statusStr)
	if err != nil {
		return 0, fmt.Errorf("invalid token status value: %w", err)
	}

	return status, nil
}

// IsTokenValid checks if token is valid (exists and has normal status)
func (s *TokenStore) IsTokenValid(ctx context.Context, userId string, platformId int, token string) (bool, error) {
	status, err := s.ValidateTokenStatus(ctx, userId, platformId, token)
	if err != nil {
		return false, err
	}
	return status == TokenStatusNormal, nil
}

// InvalidateToken marks a token as invalid (logout)
func (s *TokenStore) InvalidateToken(ctx context.Context, userId string, platformId int, token string) error {
	key := s.tokenKey(userId, platformId)

	// Check if token exists
	exists, err := s.rdb.HExists(ctx, key, token).Result()
	if err != nil {
		return fmt.Errorf("failed to check token existence: %w", err)
	}
	if !exists {
		return nil // Token doesn't exist, nothing to invalidate
	}

	// Set status to logout
	if err := s.rdb.HSet(ctx, key, token, TokenStatusLogout).Err(); err != nil {
		return fmt.Errorf("failed to invalidate token: %w", err)
	}

	return nil
}

// DeleteToken removes a token from Redis
func (s *TokenStore) DeleteToken(ctx context.Context, userId string, platformId int, token string) error {
	key := s.tokenKey(userId, platformId)

	if err := s.rdb.HDel(ctx, key, token).Err(); err != nil {
		return fmt.Errorf("failed to delete token: %w", err)
	}

	return nil
}

// KickOtherTokens marks all other tokens for this user/platform as kicked
// Returns the list of kicked tokens
func (s *TokenStore) KickOtherTokens(ctx context.Context, userId string, platformId int, currentToken string) ([]string, error) {
	key := s.tokenKey(userId, platformId)

	// Get all tokens for this user/platform
	tokens, err := s.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get tokens: %w", err)
	}

	var kickedTokens []string
	for token, statusStr := range tokens {
		if token == currentToken {
			continue // Don't kick current token
		}

		status, _ := strconv.Atoi(statusStr)
		if status == TokenStatusNormal {
			// Mark as kicked
			if err := s.rdb.HSet(ctx, key, token, TokenStatusKicked).Err(); err != nil {
				return nil, fmt.Errorf("failed to kick token: %w", err)
			}
			kickedTokens = append(kickedTokens, token)
		}
	}

	return kickedTokens, nil
}

// GetAllTokens returns all tokens for a user/platform with their status
func (s *TokenStore) GetAllTokens(ctx context.Context, userId string, platformId int) (map[string]int, error) {
	key := s.tokenKey(userId, platformId)

	tokens, err := s.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get tokens: %w", err)
	}

	result := make(map[string]int)
	for token, statusStr := range tokens {
		status, _ := strconv.Atoi(statusStr)
		result[token] = status
	}

	return result, nil
}

// CleanExpiredTokens removes tokens that are not in normal status
// This can be called periodically to clean up old tokens
func (s *TokenStore) CleanExpiredTokens(ctx context.Context, userId string, platformId int) error {
	key := s.tokenKey(userId, platformId)

	tokens, err := s.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to get tokens: %w", err)
	}

	var toDelete []string
	for token, statusStr := range tokens {
		status, _ := strconv.Atoi(statusStr)
		if status != TokenStatusNormal {
			toDelete = append(toDelete, token)
		}
	}

	if len(toDelete) > 0 {
		if err := s.rdb.HDel(ctx, key, toDelete...).Err(); err != nil {
			return fmt.Errorf("failed to delete expired tokens: %w", err)
		}
	}

	return nil
}

// ForceLogoutUser invalidates all tokens for a user across all platforms
func (s *TokenStore) ForceLogoutUser(ctx context.Context, userId string) error {
	// Scan for all platform keys for this user
	pattern := fmt.Sprintf("%s%s:*", s.keyPrefix, userId)

	var cursor uint64
	for {
		keys, nextCursor, err := s.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("failed to scan keys: %w", err)
		}

		// Delete all found keys
		if len(keys) > 0 {
			if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("failed to delete keys: %w", err)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return nil
}

// ForceLogoutPlatform invalidates all tokens for a user on a specific platform
func (s *TokenStore) ForceLogoutPlatform(ctx context.Context, userId string, platformId int) error {
	key := s.tokenKey(userId, platformId)

	if err := s.rdb.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete platform tokens: %w", err)
	}

	return nil
}
