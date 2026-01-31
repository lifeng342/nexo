package jwt

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mbeoliero/nexo/pkg/errcode"
)

// Claims represents JWT claims
type Claims struct {
	UserId     string `json:"user_id"`
	PlatformId int    `json:"platform_id"`
	jwt.RegisteredClaims
}

// GenerateToken generates a new JWT token
func GenerateToken(userId string, platformId int, secret string, expireHours int) (string, error) {
	claims := Claims{
		UserId:     userId,
		PlatformId: platformId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(expireHours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "nexo-im",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken parses and validates a JWT token
func ParseToken(tokenString, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})

	if err != nil {
		return nil, errcode.ErrTokenInvalid.Wrap(err)
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errcode.ErrTokenInvalid
}

// ValidateToken validates token and checks if userId and platformId match
func ValidateToken(tokenString, secret, expectedUserId string, expectedPlatformId int) (*Claims, error) {
	claims, err := ParseToken(tokenString, secret)
	if err != nil {
		return nil, err
	}

	if claims.UserId != expectedUserId {
		return nil, errcode.ErrTokenMismatch
	}

	if claims.PlatformId != expectedPlatformId {
		return nil, errcode.ErrTokenMismatch
	}

	return claims, nil
}
