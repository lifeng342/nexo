package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/mbeoliero/kit/log"
	"github.com/redis/go-redis/v9"

	"github.com/mbeoliero/nexo/pkg/constant"
)

// PushBus publishes and subscribes cross-instance push envelopes.
type PushBus interface {
	PublishToInstance(ctx context.Context, instanceId string, env *PushEnvelope) error
	PublishBroadcast(ctx context.Context, env *PushEnvelope) error
	SubscribeInstance(ctx context.Context, instanceId string, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) error
	SubscribeBroadcast(ctx context.Context, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) error
	Close() error
}

// RedisPushBus is the Redis Pub/Sub implementation.
type RedisPushBus struct {
	rdb       *redis.Client
	instSub   *redis.PubSub
	broadSub  *redis.PubSub
	closed    atomic.Bool
	closeOnce sync.Once
}

// NewRedisPushBus creates a Redis-backed push bus.
func NewRedisPushBus(rdb *redis.Client) *RedisPushBus {
	return &RedisPushBus{rdb: rdb}
}

// PublishToInstance publishes an envelope to one instance channel.
func (b *RedisPushBus) PublishToInstance(ctx context.Context, instanceId string, env *PushEnvelope) error {
	if b == nil || b.rdb == nil {
		return nil
	}
	if b.closed.Load() {
		return errors.New("push bus closed")
	}

	payload, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, fmt.Sprintf(constant.RedisKeyPushInstance(), instanceId), payload).Err()
}

// PublishBroadcast publishes an envelope to the shared broadcast channel.
func (b *RedisPushBus) PublishBroadcast(ctx context.Context, env *PushEnvelope) error {
	if b == nil || b.rdb == nil {
		return nil
	}
	if b.closed.Load() {
		return errors.New("push bus closed")
	}

	payload, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, constant.RedisKeyPushBroadcast(), payload).Err()
}

// SubscribeInstance subscribes to the current instance route channel.
func (b *RedisPushBus) SubscribeInstance(ctx context.Context, instanceId string, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) error {
	if b != nil && b.closed.Load() {
		return errors.New("push bus closed")
	}
	if b == nil || b.rdb == nil {
		return nil
	}

	sub := b.rdb.Subscribe(ctx, fmt.Sprintf(constant.RedisKeyPushInstance(), instanceId))
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return err
	}

	b.instSub = sub
	go b.consumeSubscription(ctx, sub, handler, onRuntimeError)
	return nil
}

// SubscribeBroadcast subscribes to the broadcast channel.
func (b *RedisPushBus) SubscribeBroadcast(ctx context.Context, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) error {
	if b != nil && b.closed.Load() {
		return errors.New("push bus closed")
	}
	if b == nil || b.rdb == nil {
		return nil
	}

	sub := b.rdb.Subscribe(ctx, constant.RedisKeyPushBroadcast())
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return err
	}

	b.broadSub = sub
	go b.consumeSubscription(ctx, sub, handler, onRuntimeError)
	return nil
}

// Close closes both subscription handles.
func (b *RedisPushBus) Close() error {
	if b == nil {
		return nil
	}

	var err error
	b.closeOnce.Do(func() {
		b.closed.Store(true)
		if b.instSub != nil {
			err = b.instSub.Close()
		}
		if b.broadSub != nil {
			closeErr := b.broadSub.Close()
			if err == nil {
				err = closeErr
			}
		}
	})
	return err
}

func (b *RedisPushBus) consumeSubscription(ctx context.Context, sub *redis.PubSub, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) {
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				if !b.closed.Load() && onRuntimeError != nil {
					onRuntimeError(errors.New("push bus subscription closed"))
				}
				return
			}

			var env PushEnvelope
			if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
				// A malformed envelope should be dropped locally instead of escalating
				// the whole subscription into a transport-level failure.
				log.Warn("drop invalid push envelope: channel=%s err=%v", msg.Channel, err)
				continue
			}
			handler(ctx, &env)
		}
	}
}
