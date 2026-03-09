package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/mbeoliero/nexo/internal/service"
	"github.com/mbeoliero/nexo/pkg/response"
	"github.com/stretchr/testify/require"
)

type stubPresenceService struct {
	results []*service.OnlineStatusResult
	meta    response.Meta
}

func (s *stubPresenceService) GetUsersOnlineStatus(ctx context.Context, userIDs []string) ([]*service.OnlineStatusResult, response.Meta, error) {
	return s.results, s.meta, nil
}

func TestGetUsersOnlineStatusRejectsMoreThan100UserIDs(t *testing.T) {
	h := &UserHandler{presence: &stubPresenceService{}}
	c := app.NewContext(0)
	ctx := context.Background()
	payload := `{"user_ids":[` + makeManyUserIDs(101) + `]}`
	c.Request.SetBody([]byte(payload))
	c.Request.Header.SetMethod("POST")
	c.Request.Header.SetContentTypeBytes([]byte("application/json"))

	h.GetUsersOnlineStatus(ctx, c)

	require.Equal(t, 200, c.Response.StatusCode())
	var body response.Response
	err := json.Unmarshal(c.Response.Body(), &body)
	require.NoError(t, err)
	require.Equal(t, 1001, body.Code)
}

func TestGetUsersOnlineStatusReturnsSuccessWithMetaOnPartial(t *testing.T) {
	h := &UserHandler{presence: &stubPresenceService{results: []*service.OnlineStatusResult{{UserId: "u1", Status: 1}}, meta: response.Meta{"partial": true, "data_source": "local_only"}}}
	c := app.NewContext(0)
	ctx := context.Background()
	c.Request.SetBody([]byte(`{"user_ids":["u1"]}`))
	c.Request.Header.SetMethod("POST")
	c.Request.Header.SetContentTypeBytes([]byte("application/json"))

	h.GetUsersOnlineStatus(ctx, c)
	require.NotEmpty(t, c.Response.Body())

	var body response.Response
	err := json.Unmarshal(c.Response.Body(), &body)
	require.NoError(t, err)
	require.Equal(t, response.Meta{"partial": true, "data_source": "local_only"}, body.Meta)
}

func makeManyUserIDs(n int) string {
	result := ""
	for i := range n {
		if i > 0 {
			result += ","
		}
		result += `"u"`
	}
	return result
}
