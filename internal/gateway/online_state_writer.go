package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/mbeoliero/nexo/pkg/constant"
)

type onlineStateEvent struct {
	userId string
	online bool
}

// OnlineStateWriter keeps the legacy online key in sync asynchronously.
type OnlineStateWriter struct {
	rdb *redis.Client
	ttl time.Duration
	ch  chan onlineStateEvent
}

// NewOnlineStateWriter creates a writer for the legacy online key.
func NewOnlineStateWriter(rdb *redis.Client, ttl time.Duration) *OnlineStateWriter {
	return &OnlineStateWriter{
		rdb: rdb,
		ttl: ttl,
		ch:  make(chan onlineStateEvent, 1024),
	}
}

// Run starts the background writer loop.
func (w *OnlineStateWriter) Run(ctx context.Context) {
	if w == nil || w.rdb == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-w.ch:
			key := fmt.Sprintf(constant.RedisKeyOnline(), evt.userId)
			if evt.online {
				_ = w.rdb.Set(ctx, key, "1", w.ttl).Err()
				continue
			}
			_ = w.rdb.Del(ctx, key).Err()
		}
	}
}

// MarkOnline schedules an online mark.
func (w *OnlineStateWriter) MarkOnline(userId string) {
	if w == nil || w.rdb == nil {
		return
	}
	select {
	case w.ch <- onlineStateEvent{userId: userId, online: true}:
	default:
	}
}

// MarkOffline schedules an offline mark.
func (w *OnlineStateWriter) MarkOffline(userId string) {
	if w == nil || w.rdb == nil {
		return
	}
	select {
	case w.ch <- onlineStateEvent{userId: userId, online: false}:
	default:
	}
}
