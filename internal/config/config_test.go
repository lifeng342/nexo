package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSetsCrossInstanceDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`server:
  http_port: 8080
redis:
  host: 127.0.0.1
  port: 6379
mysql:
  host: 127.0.0.1
  port: 3306
  user: root
  database: test
jwt:
  secret: x
`), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.False(t, cfg.WebSocket.CrossInstance.Enabled)
	require.Equal(t, 10, cfg.WebSocket.CrossInstance.HeartbeatSecond)
	require.Equal(t, 70, cfg.WebSocket.CrossInstance.RouteTTLSeconds)
}
