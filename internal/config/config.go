package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config holds all configuration
type Config struct {
	Server      ServerConfig      `mapstructure:"server"`
	MySQL       MySQLConfig       `mapstructure:"mysql"`
	Redis       RedisConfig       `mapstructure:"redis"`
	JWT         JWTConfig         `mapstructure:"jwt"`
	ExternalJWT ExternalJWTConfig `mapstructure:"external_jwt"`
	WebSocket   WebSocketConfig   `mapstructure:"websocket"`
}

// ServerConfig holds server configuration
type ServerConfig struct {
	HTTPPort       int      `mapstructure:"http_port"`
	WSPort         int      `mapstructure:"ws_port"`
	Mode           string   `mapstructure:"mode"`
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

// MySQLConfig holds MySQL configuration
type MySQLConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	Database     string `mapstructure:"database"`
	Charset      string `mapstructure:"charset"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

// DSN returns the MySQL data source name
func (c *MySQLConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		c.User, c.Password, c.Host, c.Port, c.Database, c.Charset)
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host      string `mapstructure:"host"`
	Port      int    `mapstructure:"port"`
	Password  string `mapstructure:"password"`
	DB        int    `mapstructure:"db"`
	KeyPrefix string `mapstructure:"key_prefix"`
}

// Addr returns the Redis address
func (c *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret      string `mapstructure:"secret"`
	ExpireHours int    `mapstructure:"expire_hours"`
}

// ExternalJWTConfig holds external JWT configuration for integrating with other systems
type ExternalJWTConfig struct {
	Enabled           bool   `mapstructure:"enabled"`
	Secret            string `mapstructure:"secret"`
	DefaultRole       string `mapstructure:"default_role"`        // "user" or "agent", defaults to "user"
	DefaultPlatformId int    `mapstructure:"default_platform_id"` // defaults to PlatformIdWeb(5)
}

// WebSocketConfig holds WebSocket configuration
type WebSocketConfig struct {
	MaxConnNum                     int64               `mapstructure:"max_conn_num"`
	MaxMessageSize                 int64               `mapstructure:"max_message_size"`
	WriteWait                      time.Duration       `mapstructure:"write_wait"`
	PongWait                       time.Duration       `mapstructure:"pong_wait"`
	PingPeriod                     time.Duration       `mapstructure:"ping_period"`
	PushChannelSize                int                 `mapstructure:"push_channel_size"`
	PushWorkerNum                  int                 `mapstructure:"push_worker_num"`
	WriteChannelSize               int                 `mapstructure:"write_channel_size"`
	RemoteEnvChannelSize           int                 `mapstructure:"remote_env_channel_size"`
	RemoteEnvBlockTimeoutMS        int                 `mapstructure:"remote_env_block_timeout_ms"`
	MaxBroadcastChunkBytes         int                 `mapstructure:"max_broadcast_chunk_bytes"`
	PushDedupTTLSeconds            int                 `mapstructure:"push_dedup_ttl_seconds"`
	RouteWriteQueueSize            int                 `mapstructure:"route_write_queue_size"`
	RouteWriteWorkerNum            int                 `mapstructure:"route_write_worker_num"`
	RouteStaleCleanupLimit         int                 `mapstructure:"route_stale_cleanup_limit"`
	RouteTTLRefreshIntervalSeconds int                 `mapstructure:"route_ttl_refresh_interval_seconds"`
	RouteReconcileIntervalSeconds  int                 `mapstructure:"route_reconcile_interval_seconds"`
	CrossInstance                  CrossInstanceConfig `mapstructure:"cross_instance"`
	Hybrid                         HybridConfig        `mapstructure:"hybrid"`
}

// CrossInstanceConfig holds cross-instance push configuration.
type CrossInstanceConfig struct {
	Enabled                         bool   `mapstructure:"enabled"`
	InstanceId                      string `mapstructure:"instance_id"`
	HeartbeatSeconds                int    `mapstructure:"heartbeat_second"`
	RouteTTLSeconds                 int    `mapstructure:"route_ttl_seconds"`
	InstanceAliveTTLSeconds         int    `mapstructure:"instance_alive_ttl_seconds"`
	PublishFailThreshold            int    `mapstructure:"publish_fail_threshold"`
	RouteSubscribeFailThreshold     int    `mapstructure:"route_subscribe_fail_threshold"`
	BroadcastSubscribeFailThreshold int    `mapstructure:"broadcast_subscribe_fail_threshold"`
	PushRouteFailThreshold          int    `mapstructure:"push_route_fail_threshold"`
	PresenceReadFailThreshold       int    `mapstructure:"presence_read_fail_threshold"`
	RecoverProbeIntervalSeconds     int    `mapstructure:"recover_probe_interval_seconds"`
	RouteSubscribeDrainAfterSeconds int    `mapstructure:"route_subscribe_drain_after_seconds"`
	DrainTimeoutSeconds             int    `mapstructure:"drain_timeout_seconds"`
	DrainRouteableGraceSeconds      int    `mapstructure:"drain_routeable_grace_seconds"`
	DrainKickBatchSize              int    `mapstructure:"drain_kick_batch_size"`
	DrainKickIntervalMS             int    `mapstructure:"drain_kick_interval_ms"`
}

// HybridConfig holds hybrid route/broadcast selection configuration.
type HybridConfig struct {
	Enabled                bool `mapstructure:"enabled"`
	GroupRouteMaxTargets   int  `mapstructure:"group_route_max_targets"`
	GroupRouteMaxInstances int  `mapstructure:"group_route_max_instances"`
	ForceBroadcast         bool `mapstructure:"force_broadcast"`
}

func (c WebSocketConfig) PushDedupTTL() time.Duration {
	return time.Duration(c.PushDedupTTLSeconds) * time.Second
}

func (c WebSocketConfig) RouteTTLRefreshInterval() time.Duration {
	return time.Duration(c.RouteTTLRefreshIntervalSeconds) * time.Second
}

func (c WebSocketConfig) RouteReconcileInterval() time.Duration {
	return time.Duration(c.RouteReconcileIntervalSeconds) * time.Second
}

func (c WebSocketConfig) RemoteEnvBlockTimeout() time.Duration {
	return time.Duration(c.RemoteEnvBlockTimeoutMS) * time.Millisecond
}

func (c CrossInstanceConfig) HeartbeatInterval() time.Duration {
	return time.Duration(c.HeartbeatSeconds) * time.Second
}

func (c CrossInstanceConfig) RouteTTL() time.Duration {
	return time.Duration(c.RouteTTLSeconds) * time.Second
}

func (c CrossInstanceConfig) InstanceAliveTTL() time.Duration {
	return time.Duration(c.InstanceAliveTTLSeconds) * time.Second
}

func (c CrossInstanceConfig) RecoverProbeInterval() time.Duration {
	return time.Duration(c.RecoverProbeIntervalSeconds) * time.Second
}

func (c CrossInstanceConfig) RouteSubscribeDrainAfter() time.Duration {
	return time.Duration(c.RouteSubscribeDrainAfterSeconds) * time.Second
}

func (c CrossInstanceConfig) DrainTimeout() time.Duration {
	return time.Duration(c.DrainTimeoutSeconds) * time.Second
}

func (c CrossInstanceConfig) DrainRouteableGrace() time.Duration {
	return time.Duration(c.DrainRouteableGraceSeconds) * time.Second
}

func (c CrossInstanceConfig) DrainKickInterval() time.Duration {
	return time.Duration(c.DrainKickIntervalMS) * time.Millisecond
}

// Global config instance
var GlobalConfig *Config

// Load loads configuration from file
func Load(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Set defaults
	if cfg.Server.HTTPPort == 0 {
		cfg.Server.HTTPPort = 8080
	}
	if cfg.Server.WSPort == 0 {
		cfg.Server.WSPort = cfg.Server.HTTPPort
	}
	if cfg.Server.Mode == "" {
		cfg.Server.Mode = "debug"
	}
	if cfg.MySQL.Charset == "" {
		cfg.MySQL.Charset = "utf8mb4"
	}
	if cfg.MySQL.MaxOpenConns == 0 {
		cfg.MySQL.MaxOpenConns = 100
	}
	if cfg.MySQL.MaxIdleConns == 0 {
		cfg.MySQL.MaxIdleConns = 10
	}
	if cfg.Redis.KeyPrefix == "" {
		cfg.Redis.KeyPrefix = "nexo:"
	}
	if cfg.JWT.ExpireHours == 0 {
		cfg.JWT.ExpireHours = 168 // 7 days
	}
	if cfg.ExternalJWT.DefaultRole == "" {
		cfg.ExternalJWT.DefaultRole = "user"
	}
	if cfg.ExternalJWT.DefaultPlatformId == 0 {
		cfg.ExternalJWT.DefaultPlatformId = 1 // ios
	}
	if cfg.WebSocket.MaxConnNum == 0 {
		cfg.WebSocket.MaxConnNum = 10000
	}
	if cfg.WebSocket.MaxMessageSize == 0 {
		cfg.WebSocket.MaxMessageSize = 51200
	}
	if cfg.WebSocket.WriteWait == 0 {
		cfg.WebSocket.WriteWait = 10 * time.Second
	}
	if cfg.WebSocket.PongWait == 0 {
		cfg.WebSocket.PongWait = 30 * time.Second
	}
	if cfg.WebSocket.PingPeriod == 0 {
		cfg.WebSocket.PingPeriod = 27 * time.Second
	}
	if cfg.WebSocket.PushChannelSize == 0 {
		cfg.WebSocket.PushChannelSize = 10000
	}
	if cfg.WebSocket.PushWorkerNum == 0 {
		cfg.WebSocket.PushWorkerNum = 10
	}
	if cfg.WebSocket.WriteChannelSize == 0 {
		cfg.WebSocket.WriteChannelSize = 256
	}
	if cfg.WebSocket.RemoteEnvChannelSize == 0 {
		cfg.WebSocket.RemoteEnvChannelSize = 1024
	}
	if cfg.WebSocket.RemoteEnvBlockTimeoutMS == 0 {
		cfg.WebSocket.RemoteEnvBlockTimeoutMS = 50
	}
	if cfg.WebSocket.MaxBroadcastChunkBytes == 0 {
		cfg.WebSocket.MaxBroadcastChunkBytes = 128 * 1024
	}
	if cfg.WebSocket.PushDedupTTLSeconds == 0 {
		cfg.WebSocket.PushDedupTTLSeconds = 300
	}
	if cfg.WebSocket.RouteWriteQueueSize == 0 {
		cfg.WebSocket.RouteWriteQueueSize = 4096
	}
	if cfg.WebSocket.RouteWriteWorkerNum == 0 {
		cfg.WebSocket.RouteWriteWorkerNum = 4
	}
	if cfg.WebSocket.RouteStaleCleanupLimit == 0 {
		cfg.WebSocket.RouteStaleCleanupLimit = 64
	}
	if cfg.WebSocket.RouteTTLRefreshIntervalSeconds == 0 {
		cfg.WebSocket.RouteTTLRefreshIntervalSeconds = 20
	}
	if cfg.WebSocket.RouteReconcileIntervalSeconds == 0 {
		cfg.WebSocket.RouteReconcileIntervalSeconds = 60
	}
	if cfg.WebSocket.CrossInstance.HeartbeatSeconds == 0 {
		cfg.WebSocket.CrossInstance.HeartbeatSeconds = 10
	}
	if cfg.WebSocket.CrossInstance.RouteTTLSeconds == 0 {
		cfg.WebSocket.CrossInstance.RouteTTLSeconds = 70
	}
	if cfg.WebSocket.CrossInstance.InstanceAliveTTLSeconds == 0 {
		cfg.WebSocket.CrossInstance.InstanceAliveTTLSeconds = 30
	}
	if cfg.WebSocket.CrossInstance.PublishFailThreshold == 0 {
		cfg.WebSocket.CrossInstance.PublishFailThreshold = 3
	}
	if cfg.WebSocket.CrossInstance.RouteSubscribeFailThreshold == 0 {
		cfg.WebSocket.CrossInstance.RouteSubscribeFailThreshold = 1
	}
	if cfg.WebSocket.CrossInstance.BroadcastSubscribeFailThreshold == 0 {
		cfg.WebSocket.CrossInstance.BroadcastSubscribeFailThreshold = 3
	}
	if cfg.WebSocket.CrossInstance.PushRouteFailThreshold == 0 {
		cfg.WebSocket.CrossInstance.PushRouteFailThreshold = 3
	}
	if cfg.WebSocket.CrossInstance.PresenceReadFailThreshold == 0 {
		cfg.WebSocket.CrossInstance.PresenceReadFailThreshold = 3
	}
	if cfg.WebSocket.CrossInstance.RecoverProbeIntervalSeconds == 0 {
		cfg.WebSocket.CrossInstance.RecoverProbeIntervalSeconds = 5
	}
	if cfg.WebSocket.CrossInstance.RouteSubscribeDrainAfterSeconds == 0 {
		cfg.WebSocket.CrossInstance.RouteSubscribeDrainAfterSeconds = 60
	}
	if cfg.WebSocket.CrossInstance.DrainTimeoutSeconds == 0 {
		cfg.WebSocket.CrossInstance.DrainTimeoutSeconds = 30
	}
	if cfg.WebSocket.CrossInstance.DrainRouteableGraceSeconds == 0 {
		cfg.WebSocket.CrossInstance.DrainRouteableGraceSeconds = 15
	}
	if cfg.WebSocket.CrossInstance.DrainKickBatchSize == 0 {
		cfg.WebSocket.CrossInstance.DrainKickBatchSize = 200
	}
	if cfg.WebSocket.CrossInstance.DrainKickIntervalMS == 0 {
		cfg.WebSocket.CrossInstance.DrainKickIntervalMS = 100
	}
	if cfg.WebSocket.Hybrid.GroupRouteMaxTargets == 0 {
		cfg.WebSocket.Hybrid.GroupRouteMaxTargets = 100
	}
	if cfg.WebSocket.Hybrid.GroupRouteMaxInstances == 0 {
		cfg.WebSocket.Hybrid.GroupRouteMaxInstances = 4
	}

	GlobalConfig = &cfg
	return &cfg, nil
}
