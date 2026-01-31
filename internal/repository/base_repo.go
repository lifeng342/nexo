package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/mbeoliero/kit/log"
	"github.com/mbeoliero/nexo/internal/config"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Repositories holds all repositories
type Repositories struct {
	DB           *gorm.DB
	Redis        *redis.Client
	User         *UserRepo
	Group        *GroupRepo
	Message      *MessageRepo
	Conversation *ConversationRepo
	Seq          *SeqRepo
}

// NewRepositories creates all repositories
func NewRepositories(cfg *config.Config) (*Repositories, error) {
	// Initialize MySQL
	db, err := initMySQL(cfg)
	if err != nil {
		return nil, err
	}

	// Initialize Redis
	rdb := initRedis(cfg)

	repos := &Repositories{
		DB:    db,
		Redis: rdb,
	}

	// Initialize individual repositories
	repos.User = NewUserRepo(db, rdb)
	repos.Group = NewGroupRepo(db, rdb)
	repos.Message = NewMessageRepo(db, rdb)
	repos.Conversation = NewConversationRepo(db, rdb)
	repos.Seq = NewSeqRepo(db, rdb)

	return repos, nil
}

// initMySQL initializes MySQL connection
func initMySQL(cfg *config.Config) (*gorm.DB, error) {
	var logLevel logger.LogLevel
	if cfg.Server.Mode == "debug" {
		logLevel = logger.Info
	} else {
		logLevel = logger.Warn
	}

	db, err := gorm.Open(mysql.Open(cfg.MySQL.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(cfg.MySQL.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MySQL.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}

// initRedis initializes Redis connection
func initRedis(cfg *config.Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr(),
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
}

// Close closes all connections
func (r *Repositories) Close() error {
	sqlDB, err := r.DB.DB()
	if err != nil {
		return err
	}
	if err := sqlDB.Close(); err != nil {
		return err
	}
	return r.Redis.Close()
}

// Transaction executes fn in a transaction
func (r *Repositories) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.DB.WithContext(ctx).Transaction(fn)
}

// TransactionWithOptions executes fn in a transaction with options
func (r *Repositories) TransactionWithOptions(ctx context.Context, opts *sql.TxOptions, fn func(tx *gorm.DB) error) error {
	return r.DB.WithContext(ctx).Transaction(fn, opts)
}

// CheckConnection checks if database and redis connections are alive
func (r *Repositories) CheckConnection(ctx context.Context) error {
	// Check MySQL
	sqlDB, err := r.DB.DB()
	if err != nil {
		return err
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		log.CtxError(ctx, "mysql ping failed: %v", err)
		return err
	}

	// Check Redis
	if err := r.Redis.Ping(ctx).Err(); err != nil {
		log.CtxError(ctx, "redis ping failed: %v", err)
		return err
	}

	return nil
}
