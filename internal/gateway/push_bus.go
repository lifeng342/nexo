package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/mbeoliero/nexo/pkg/constant"
	"github.com/redis/go-redis/v9"
)

var ErrPushBusClosed = errors.New("push bus closed")

type PushBus interface {
	PublishToInstance(ctx context.Context, instanceID string, env *PushEnvelope) error
	SubscribeInstance(ctx context.Context, instanceID string, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) error
	Close() error
}

type InMemoryPushBus struct {
	mu          sync.RWMutex
	closed      bool
	subscribers map[string][]func(context.Context, *PushEnvelope)
}

func NewInMemoryPushBus() *InMemoryPushBus {
	return &InMemoryPushBus{subscribers: make(map[string][]func(context.Context, *PushEnvelope))}
}

func (b *InMemoryPushBus) PublishToInstance(ctx context.Context, instanceID string, env *PushEnvelope) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrPushBusClosed
	}
	for _, handler := range b.subscribers[instanceID] {
		handler(ctx, env)
	}
	return nil
}

func (b *InMemoryPushBus) SubscribeInstance(ctx context.Context, instanceID string, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrPushBusClosed
	}
	b.subscribers[instanceID] = append(b.subscribers[instanceID], handler)
	return nil
}

func (b *InMemoryPushBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

type RedisPushBus struct {
	rdb    *redis.Client
	mu     sync.Mutex
	closed bool
	pubsubs []*redis.PubSub
}

func NewRedisPushBus(rdb *redis.Client) *RedisPushBus {
	return &RedisPushBus{rdb: rdb}
}

func pushBusChannel(instanceID string) string {
	return fmt.Sprintf("%spush:route:%s", constant.GetRedisKeyPrefix(), instanceID)
}

func (b *RedisPushBus) PublishToInstance(ctx context.Context, instanceID string, env *PushEnvelope) error {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return ErrPushBusClosed
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, pushBusChannel(instanceID), data).Err()
}

func (b *RedisPushBus) SubscribeInstance(ctx context.Context, instanceID string, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrPushBusClosed
	}
	pubsub := b.rdb.Subscribe(ctx, pushBusChannel(instanceID))
	b.pubsubs = append(b.pubsubs, pubsub)
	b.mu.Unlock()
	ch := pubsub.Channel()
	go func() {
		defer pubsub.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					if ctx.Err() != nil {
						return
					}
					if onRuntimeError != nil {
						onRuntimeError(ErrPushBusClosed)
					}
					return
				}
				var env PushEnvelope
				if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
					continue
				}
				handler(ctx, &env)
			}
		}
	}()
	return nil
}

func (b *RedisPushBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for _, pubsub := range b.pubsubs {
		_ = pubsub.Close()
	}
	return nil
}
