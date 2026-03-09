package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/stretchr/testify/require"
)

type stubRouteStore struct {
	routeMap map[string][]RouteConn
	err      error
}

func (s *stubRouteStore) GetUsersConnRefs(ctx context.Context, userIDs []string) (map[string][]RouteConn, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.routeMap, nil
}

type recordingLocalPusher struct {
	calls []string
}

func (r *recordingLocalPusher) PushToLocalClients(ctx context.Context, task *PushTask, msgData *MessageData) {
	r.calls = append(r.calls, "local")
}

func (r *recordingLocalPusher) PushToConnRefs(ctx context.Context, refs []ConnRef, msgData *MessageData) {
	r.calls = append(r.calls, "local")
}

type recordingBus struct {
	envs []*PushEnvelope
}

func (b *recordingBus) PublishToInstance(ctx context.Context, instanceID string, env *PushEnvelope) error {
	b.envs = append(b.envs, env)
	return nil
}
func (b *recordingBus) SubscribeInstance(ctx context.Context, instanceID string, handler func(context.Context, *PushEnvelope), onRuntimeError func(error)) error {
	return nil
}
func (b *recordingBus) Close() error { return nil }

func TestProcessPushTaskPushesLocalBeforePublishingRoute(t *testing.T) {
	local := &recordingLocalPusher{}
	bus := &recordingBus{}
	coordinator := NewPushCoordinator("inst-local", nil, &stubRouteStore{routeMap: map[string][]RouteConn{"u1": {{UserId: "u1", ConnId: "c1", InstanceId: "inst-remote", PlatformId: 1}}}}, bus, local)
	msg := &entity.Message{Id: 1, ConversationId: "c1", Seq: 1, ClientMsgId: "m1", SenderId: "u0", SendAt: time.Now().UnixMilli()}

	coordinator.processPushTask(context.Background(), &PushTask{Msg: msg, TargetIds: []string{"u1"}})

	require.Equal(t, []string{"local"}, local.calls)
	require.Len(t, bus.envs, 1)
}

func TestDispatchRouteOnlyPublishesPerInstanceEnvelope(t *testing.T) {
	bus := &recordingBus{}
	coordinator := NewPushCoordinator("inst-local", nil, &stubRouteStore{routeMap: map[string][]RouteConn{"u1": {{UserId: "u1", ConnId: "c1", InstanceId: "inst-a", PlatformId: 1}, {UserId: "u1", ConnId: "c2", InstanceId: "inst-b", PlatformId: 2}}}}, bus, &recordingLocalPusher{})
	msg := &entity.Message{Id: 1, ConversationId: "c1", Seq: 1, ClientMsgId: "m1", SenderId: "u0", SendAt: time.Now().UnixMilli()}

	coordinator.dispatchRouteOnly(context.Background(), &PushTask{Msg: msg, TargetIds: []string{"u1"}}, coordinator.toPushPayload(msg))

	require.Len(t, bus.envs, 2)
}

func TestRouteReadFailureFallsBackToLocalOnly(t *testing.T) {
	local := &recordingLocalPusher{}
	bus := &recordingBus{}
	coordinator := NewPushCoordinator("inst-local", nil, &stubRouteStore{err: context.DeadlineExceeded}, bus, local)
	msg := &entity.Message{Id: 1, ConversationId: "c1", Seq: 1, ClientMsgId: "m1", SenderId: "u0", SendAt: time.Now().UnixMilli()}

	coordinator.processPushTask(context.Background(), &PushTask{Msg: msg, TargetIds: []string{"u1"}})

	require.Equal(t, []string{"local"}, local.calls)
	require.Len(t, bus.envs, 0)
}

func TestRemoteEnvelopeDeliversWithoutRedisLookup(t *testing.T) {
	local := &recordingLocalPusher{}
	coordinator := NewPushCoordinator("inst-local", nil, nil, nil, local)
	env := &PushEnvelope{PushId: "p1", Mode: PushModeRoute, TargetConnMap: map[string][]ConnRef{"inst-local": {{UserId: "u1", ConnId: "c1", PlatformId: 1}}}, Payload: &PushPayload{MsgId: 1, ConversationId: "c1", Seq: 1, ClientMsgId: "m1", SenderId: "u0", SendAt: time.Now().UnixMilli()}}

	coordinator.OnRemoteEnvelope(context.Background(), env)

	require.Equal(t, []string{"local"}, local.calls)
}
