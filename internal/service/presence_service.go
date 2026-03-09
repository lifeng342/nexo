package service

import (
	"context"

	"github.com/mbeoliero/kit/log"
	"github.com/mbeoliero/nexo/pkg/constant"
	"github.com/mbeoliero/nexo/pkg/response"
)

type RouteConnRef struct {
	UserId     string
	ConnId     string
	InstanceId string
	PlatformId int
}

type PlatformStatusDetail struct {
	PlatformId   int    `json:"platform_id"`
	PlatformName string `json:"platform_name"`
	ConnId       string `json:"conn_id"`
}

type OnlineStatusResult struct {
	UserId               string                  `json:"user_id"`
	Status               int                     `json:"status"`
	DetailPlatformStatus []*PlatformStatusDetail `json:"detail_platform_status,omitempty"`
}

type LocalPresenceSnapshot func() map[string][]RouteConnRef

type RemotePresenceLookup func(ctx context.Context, userIDs []string) (map[string][]RouteConnRef, error)

type PresenceService struct {
	localSnapshot LocalPresenceSnapshot
	remoteLookup  RemotePresenceLookup
	instanceID    string
}

func NewPresenceService(localSnapshot LocalPresenceSnapshot, remoteLookup RemotePresenceLookup, instanceID string) *PresenceService {
	return &PresenceService{localSnapshot: localSnapshot, remoteLookup: remoteLookup, instanceID: instanceID}
}

func (s *PresenceService) GetUsersOnlineStatus(ctx context.Context, userIDs []string) ([]*OnlineStatusResult, response.Meta, error) {
	localMap := map[string][]RouteConnRef{}
	if s.localSnapshot != nil {
		localMap = s.localSnapshot()
	}

	remoteMap := map[string][]RouteConnRef{}
	var meta response.Meta
	if s.remoteLookup != nil {
		result, err := s.remoteLookup(ctx, userIDs)
		if err != nil {
			log.CtxWarn(ctx, "remote presence lookup failed: user_count=%d, error=%v", len(userIDs), err)
			meta = response.Meta{"partial": true, "data_source": "local_only", "reason": "remote_presence_lookup_failed"}
		} else {
			remoteMap = result
		}
	}

	results := make([]*OnlineStatusResult, 0, len(userIDs))
	for _, userID := range userIDs {
		merged := make([]RouteConnRef, 0)
		merged = append(merged, localMap[userID]...)
		for _, conn := range remoteMap[userID] {
			if conn.InstanceId == s.instanceID {
				continue
			}
			merged = append(merged, conn)
		}

		result := &OnlineStatusResult{UserId: userID, Status: constant.StatusOffline}
		if len(merged) > 0 {
			result.Status = constant.StatusOnline
			result.DetailPlatformStatus = make([]*PlatformStatusDetail, 0, len(merged))
			for _, conn := range merged {
				result.DetailPlatformStatus = append(result.DetailPlatformStatus, &PlatformStatusDetail{
					PlatformId:   conn.PlatformId,
					PlatformName: constant.PlatformIdToName(conn.PlatformId),
					ConnId:       conn.ConnId,
				})
			}
		}
		results = append(results, result)
	}

	return results, meta, nil
}
