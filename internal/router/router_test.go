package router

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/mbeoliero/nexo/internal/handler"
	"github.com/stretchr/testify/require"
)

type stubReadiness struct{ ready bool }

func (s stubReadiness) IsReady() bool { return s.ready }

func TestReadyEndpointReturns503WhenGateNotReady(t *testing.T) {
	h := server.New()
	SetupRouter(h, &Handlers{Message: &handler.MessageHandler{}}, nil, stubReadiness{ready: false})

	ctx := context.Background()
	c := app.NewContext(0)
	c.Request.SetRequestURI("/ready")
	c.Request.Header.SetMethod("GET")

	h.ServeHTTP(ctx, c)

	require.Equal(t, 503, c.Response.StatusCode())
	var body map[string]string
	err := json.Unmarshal(c.Response.Body(), &body)
	require.NoError(t, err)
	require.Equal(t, "not_ready", body["status"])
}
