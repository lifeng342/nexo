package response

import (
	"context"
	"errors"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"

	"github.com/mbeoliero/nexo/pkg/errcode"
)

// Response represents a standard API response
type Meta map[string]any

// Response represents a standard API response
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Meta    Meta   `json:"meta,omitempty"`
}

// Success sends a success response
func Success(ctx context.Context, c *app.RequestContext, data any) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

// SuccessWithMeta sends a success response with meta information.
func SuccessWithMeta(ctx context.Context, c *app.RequestContext, data any, meta Meta) {
	c.JSON(http.StatusOK, Response{
		Code:    0,
		Message: "success",
		Data:    data,
		Meta:    meta,
	})
}

// Error sends an error response
func Error(ctx context.Context, c *app.RequestContext, err error) {
	status := http.StatusOK
	var code int
	var msg string

	var e *errcode.Error
	if errors.As(err, &e) {
		code = e.Code
		msg = e.Msg
		if e == errcode.ErrServerShuttingDown {
			status = http.StatusServiceUnavailable
		}
	}

	c.JSON(status, Response{
		Code:    code,
		Message: msg,
	})
}

// ErrorWithCode sends an error response with specific error code
func ErrorWithCode(ctx context.Context, c *app.RequestContext, e *errcode.Error) {
	status := http.StatusOK
	if e == errcode.ErrServerShuttingDown {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, Response{
		Code:    e.Code,
		Message: e.Msg,
	})
}

// Unauthorized sends a 401 unauthorized response
func Unauthorized(ctx context.Context, c *app.RequestContext, msg string) {
	if msg == "" {
		msg = "unauthorized"
	}
	c.JSON(http.StatusUnauthorized, Response{
		Code:    errcode.ErrUnauthorized.Code,
		Message: msg,
	})
}

// Forbidden sends a 403 forbidden response
func Forbidden(ctx context.Context, c *app.RequestContext, msg string) {
	if msg == "" {
		msg = "forbidden"
	}
	c.JSON(http.StatusForbidden, Response{
		Code:    errcode.ErrForbidden.Code,
		Message: msg,
	})
}
