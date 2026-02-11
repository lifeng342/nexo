package common

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// GeneratePasswordFromUserId generates a deterministic shorter password from user ID using HMAC-SHA256.
// This is used for third-party system integration where users are auto-created.
//
// Parameters:
//   - userId: The IM system user ID (e.g., "u___42" or "ag__7")
//   - secret: A secret key from configuration (should be kept secure)
//   - nBytes: number of bytes to keep from HMAC (e.g. 12 or 16)
//
// Returns:
//   - A shorter deterministic string that can be used as password
//
// Example:
//
//	password := GeneratePasswordFromUserId("u___42", "my-secret-key", 12)
//	// password will always be the same for "u___42" with the same secret
func GeneratePasswordFromUserId(userId, secret string, nBytes int) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(userId))
	sum := mac.Sum(nil) // 32 bytes
	if nBytes <= 0 || nBytes > len(sum) {
		nBytes = 16
	}
	short := sum[:nBytes]
	// base64url without padding, safe for most systems
	return base64.RawURLEncoding.EncodeToString(short)

}
