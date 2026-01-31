package response

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/mbeoliero/nexo/pkg/errcode"
)

// Response represents a standard API response
type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

// Success sends a success response
func Success(ctx context.Context, c *app.RequestContext, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code: 0,
		Msg:  "success",
		Data: data,
	})
}

// Error sends an error response
func Error(ctx context.Context, c *app.RequestContext, err error) {
	var code int
	var msg string

	if e, ok := err.(*errcode.Error); ok {
		code = e.Code
		msg = e.Msg
	} else {
		code = errcode.ErrInternalServer.Code
		msg = err.Error()
	}

	c.JSON(http.StatusOK, Response{
		Code: code,
		Msg:  msg,
	})
}

// ErrorWithCode sends an error response with specific error code
func ErrorWithCode(ctx context.Context, c *app.RequestContext, e *errcode.Error) {
	c.JSON(http.StatusOK, Response{
		Code: e.Code,
		Msg:  e.Msg,
	})
}

// Unauthorized sends a 401 unauthorized response
func Unauthorized(ctx context.Context, c *app.RequestContext, msg string) {
	if msg == "" {
		msg = "unauthorized"
	}
	c.JSON(http.StatusUnauthorized, Response{
		Code: errcode.ErrUnauthorized.Code,
		Msg:  msg,
	})
}

// Forbidden sends a 403 forbidden response
func Forbidden(ctx context.Context, c *app.RequestContext, msg string) {
	if msg == "" {
		msg = "forbidden"
	}
	c.JSON(http.StatusForbidden, Response{
		Code: errcode.ErrForbidden.Code,
		Msg:  msg,
	})
}
