package gateway

import "errors"

// Gateway errors
var (
	ErrConnClosed       = errors.New("connection closed")
	ErrWriteChannelFull = errors.New("write channel full")
	ErrInvalidProtocol  = errors.New("invalid protocol")
	ErrUserIdMismatch   = errors.New("user Id mismatch")
	ErrTokenInvalid     = errors.New("token invalid")
	ErrPanic            = errors.New("panic error")
)
