package sdk

import "fmt"

// Error represents an API error
type Error struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("code: %d, msg: %s", e.Code, e.Msg)
}

// NewError creates a new error
func NewError(code int, msg string) *Error {
	return &Error{Code: code, Msg: msg}
}

// IsSuccess checks if the error code indicates success
func (e *Error) IsSuccess() bool {
	return e.Code == 0
}

// Common error codes
const (
	// Success
	CodeSuccess = 0

	// Common errors (1xxx)
	CodeInvalidParam    = 1001
	CodeInternalServer  = 1002
	CodeUnauthorized    = 1003
	CodeForbidden       = 1004
	CodeNotFound        = 1005
	CodeTooManyRequests = 1006
	CodeNoPermission    = 1007

	// Auth errors (2xxx)
	CodeTokenInvalid  = 2001
	CodeTokenExpired  = 2002
	CodeTokenMissing  = 2003
	CodeTokenMismatch = 2004
	CodeLoginFailed   = 2005
	CodeUserNotFound  = 2006
	CodeUserExists    = 2007
	CodePasswordWrong = 2008

	// Group errors (3xxx)
	CodeGroupNotFound      = 3001
	CodeGroupDismissed     = 3002
	CodeNotGroupMember     = 3003
	CodeMemberNotActive    = 3004
	CodeAlreadyGroupMember = 3005
	CodeNotGroupOwner      = 3006
	CodeNotGroupAdmin      = 3007
	CodeCannotKickOwner    = 3008

	// Message errors (4xxx)
	CodeMessageNotFound  = 4001
	CodeMessageDuplicate = 4002
	CodeConvNotFound     = 4003
	CodeSeqAllocFailed   = 4004
	CodeSendFailed       = 4005
	CodePullFailed       = 4006

	// WebSocket errors (5xxx)
	CodeConnOverLimit   = 5001
	CodeConnClosed      = 5002
	CodeInvalidProtocol = 5003
	CodePushFailed      = 5004
)

// Predefined errors
var (
	ErrInvalidParam    = NewError(CodeInvalidParam, "invalid parameter")
	ErrInternalServer  = NewError(CodeInternalServer, "internal server error")
	ErrUnauthorized    = NewError(CodeUnauthorized, "unauthorized")
	ErrForbidden       = NewError(CodeForbidden, "forbidden")
	ErrNotFound        = NewError(CodeNotFound, "not found")
	ErrTooManyRequests = NewError(CodeTooManyRequests, "too many requests")
	ErrNoPermission    = NewError(CodeNoPermission, "no permission to access this resource")

	ErrTokenInvalid  = NewError(CodeTokenInvalid, "token invalid")
	ErrTokenExpired  = NewError(CodeTokenExpired, "token expired")
	ErrTokenMissing  = NewError(CodeTokenMissing, "token missing")
	ErrUserNotFound  = NewError(CodeUserNotFound, "user not found")
	ErrUserExists    = NewError(CodeUserExists, "user already exists")
	ErrPasswordWrong = NewError(CodePasswordWrong, "password wrong")

	ErrGroupNotFound      = NewError(CodeGroupNotFound, "group not found")
	ErrGroupDismissed     = NewError(CodeGroupDismissed, "group has been dismissed")
	ErrNotGroupMember     = NewError(CodeNotGroupMember, "not a group member")
	ErrAlreadyGroupMember = NewError(CodeAlreadyGroupMember, "already a group member")
)
