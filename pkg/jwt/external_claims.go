package jwt

import (
	"github.com/golang-jwt/jwt/v5"

	"github.com/mbeoliero/nexo/common"
	"github.com/mbeoliero/nexo/pkg/errcode"
)

// ExternalClaims represents claims from an external system.
// The external token carries an int user_id which will be converted
// to the IM string user_id via common.Actor.
type ExternalClaims struct {
	UserId int64  `json:"user_id"`
	Role   string `json:"role,omitempty"` // "user", "agent", etc. Falls back to configured default.
	jwt.RegisteredClaims
}

// ParseExternalToken parses an external system's JWT token and converts it
// to the IM system's Claims using Actor-based ID mapping.
//
// Parameters:
//   - tokenString: the raw JWT token from the external system
//   - secret: the signing secret of the external system
//   - issuer: expected issuer (empty string to skip issuer check)
//   - defaultRole: fallback role when the token doesn't carry one
//   - defaultPlatformId: platform ID to assign to the converted claims
func ParseExternalToken(tokenString, secret, defaultRole string, defaultPlatformId int) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &ExternalClaims{}, func(token *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, errcode.ErrTokenInvalid.Wrap(err)
	}

	extClaims, ok := token.Claims.(*ExternalClaims)
	if !ok || !token.Valid {
		return nil, errcode.ErrTokenInvalid
	}

	// Determine role: prefer token's own role, fall back to config default
	role := common.RoleType(extClaims.Role)
	if extClaims.Role == "" {
		role = common.RoleType(defaultRole)
	}

	// Convert external int ID → IM string ID via Actor
	actor := common.Actor{Id: extClaims.UserId, Role: role}
	imUserId, err := actor.ToIMUserId()
	if err != nil {
		return nil, errcode.ErrTokenInvalid.Wrap(err)
	}

	return &Claims{
		UserId:           imUserId,
		PlatformId:       defaultPlatformId,
		RegisteredClaims: extClaims.RegisteredClaims,
	}, nil
}
