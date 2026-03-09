package response

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/mbeoliero/nexo/pkg/errcode"
	"github.com/stretchr/testify/require"
)

func TestErrorUses503ForServerShuttingDown(t *testing.T) {
	ctx := context.Background()
	c := app.NewContext(0)

	Error(ctx, c, errcode.ErrServerShuttingDown)

	require.Equal(t, 503, c.Response.StatusCode())

	var body Response
	err := json.Unmarshal(c.Response.Body(), &body)
	require.NoError(t, err)
	require.Equal(t, errcode.ErrServerShuttingDown.Code, body.Code)
	require.Equal(t, errcode.ErrServerShuttingDown.Msg, body.Message)
}

func TestSuccessWithMeta(t *testing.T) {
	ctx := context.Background()
	c := app.NewContext(0)
	meta := Meta{"partial": true, "data_source": "local_only"}

	SuccessWithMeta(ctx, c, map[string]string{"status": "ok"}, meta)

	require.Equal(t, 200, c.Response.StatusCode())

	var body Response
	err := json.Unmarshal(c.Response.Body(), &body)
	require.NoError(t, err)
	require.Equal(t, 0, body.Code)
	require.Equal(t, "success", body.Message)
	require.Equal(t, meta, body.Meta)
}
