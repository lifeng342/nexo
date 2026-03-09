package gateway

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/mbeoliero/kit/log"
	"github.com/mbeoliero/nexo/internal/entity"
)

type routeReader interface {
	GetUsersConnRefs(ctx context.Context, userIDs []string) (map[string][]RouteConn, error)
}

type localPushExecutor interface {
	PushToLocalClients(ctx context.Context, task *PushTask, msgData *MessageData)
	PushToConnRefs(ctx context.Context, refs []ConnRef, msgData *MessageData)
}

type PushCoordinator struct {
	instanceID string
	gate       LifecycleGate
	routeStore routeReader
	pushBus    PushBus
	local      localPushExecutor
}

func NewPushCoordinator(instanceID string, gate LifecycleGate, routeStore routeReader, pushBus PushBus, local localPushExecutor) *PushCoordinator {
	return &PushCoordinator{instanceID: instanceID, gate: gate, routeStore: routeStore, pushBus: pushBus, local: local}
}

func (c *PushCoordinator) AsyncPushToUsers(msg *entity.Message, userIDs []string, excludeConnID string) {
	if c.gate != nil && !c.gate.CanStartSend() {
		return
	}
	if c.local == nil {
		return
	}
	if pusher, ok := c.local.(interface{ AsyncPushToUsers(*entity.Message, []string, string) }); ok {
		pusher.AsyncPushToUsers(msg, userIDs, excludeConnID)
		return
	}
	c.processPushTask(context.Background(), &PushTask{Msg: msg, TargetIds: userIDs, ExcludeId: excludeConnID})
}

func (c *PushCoordinator) processPushTask(ctx context.Context, task *PushTask) {
	payload := c.toPushPayload(task.Msg)
	msgData := c.toMessageData(payload)
	if c.local != nil {
		c.local.PushToLocalClients(ctx, task, msgData)
	}
	if c.routeStore == nil || c.pushBus == nil {
		return
	}
	c.dispatchRouteOnly(ctx, task, payload)
}

func (c *PushCoordinator) dispatchRouteOnly(ctx context.Context, task *PushTask, payload *PushPayload) {
	routeMap, err := c.routeStore.GetUsersConnRefs(ctx, task.TargetIds)
	if err != nil {
		return
	}
	grouped := make(map[string][]ConnRef)
	for _, refs := range routeMap {
		for _, ref := range refs {
			if ref.InstanceId == c.instanceID {
				continue
			}
			if task.ExcludeId != "" && ref.ConnId == task.ExcludeId {
				continue
			}
			grouped[ref.InstanceId] = append(grouped[ref.InstanceId], ConnRef{UserId: ref.UserId, ConnId: ref.ConnId, PlatformId: ref.PlatformId})
		}
	}
	for instID, refs := range grouped {
		err := c.pushBus.PublishToInstance(ctx, instID, &PushEnvelope{
			PushId:         uuid.New().String(),
			Mode:           PushModeRoute,
			TargetConnMap:  map[string][]ConnRef{instID: refs},
			SourceInstance: c.instanceID,
			SentAt:         nowMillis(),
			Payload:        payload,
		})
		if err != nil {
			log.CtxWarn(ctx, "publish route envelope failed: source_instance=%s, target_instance=%s, error=%v", c.instanceID, instID, err)
		}
	}
}

func (c *PushCoordinator) OnRemoteEnvelope(ctx context.Context, env *PushEnvelope) {
	if c.local == nil || env == nil || env.Payload == nil {
		return
	}
	msgData := c.toMessageData(env.Payload)
	refs := env.TargetConnMap[c.instanceID]
	c.local.PushToConnRefs(ctx, refs, msgData)
}

func (c *PushCoordinator) toPushPayload(msg *entity.Message) *PushPayload {
	if msg == nil {
		return &PushPayload{}
	}
	payload := &PushPayload{MsgId: msg.Id, ConversationId: msg.ConversationId, Seq: msg.Seq, ClientMsgId: msg.ClientMsgId, SenderId: msg.SenderId, RecvId: msg.RecvId, GroupId: msg.GroupId, SessionType: msg.SessionType, MsgType: msg.MsgType, SendAt: msg.SendAt}
	payload.Content = PushContent{Text: msg.ContentText, Image: msg.ContentImage, Video: msg.ContentVideo, Audio: msg.ContentAudio, File: msg.ContentFile}
	if msg.ContentCustom != nil {
		payload.Content.Custom = *msg.ContentCustom
	}
	return payload
}

func (c *PushCoordinator) toMessageData(payload *PushPayload) *MessageData {
	if payload == nil {
		return &MessageData{}
	}
	msg := &MessageData{ServerMsgId: payload.MsgId, ConversationId: payload.ConversationId, Seq: payload.Seq, ClientMsgId: payload.ClientMsgId, SenderId: payload.SenderId, RecvId: payload.RecvId, GroupId: payload.GroupId, SessionType: payload.SessionType, MsgType: payload.MsgType, SendAt: payload.SendAt}
	msg.Content.Text = payload.Content.Text
	msg.Content.Image = payload.Content.Image
	msg.Content.Video = payload.Content.Video
	msg.Content.Audio = payload.Content.Audio
	msg.Content.File = payload.Content.File
	msg.Content.Custom = payload.Content.Custom
	return msg
}

var _ PushBus = (*InMemoryPushBus)(nil)
var _ = errors.New
