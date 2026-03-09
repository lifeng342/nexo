package service

import (
	"context"
	"errors"
	"testing"

	"github.com/mbeoliero/nexo/pkg/constant"
	"github.com/mbeoliero/nexo/pkg/response"
	"github.com/stretchr/testify/require"
)

type stubLocalPresenceReader struct {
	users map[string][]RouteConnRef
}

func (s *stubLocalPresenceReader) SnapshotOnlineUsers() map[string][]RouteConnRef {
	return s.users
}

type stubRemotePresenceReader struct {
	result map[string][]RouteConnRef
	err    error
}

func (s *stubRemotePresenceReader) GetUsersPresenceConnRefs(ctx context.Context, userIDs []string) (map[string][]RouteConnRef, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func TestGetUsersOnlineStatusMergesLocalAndRemotePresence(t *testing.T) {
	local := &stubLocalPresenceReader{users: map[string][]RouteConnRef{"u1": {{UserId: "u1", ConnId: "c1", InstanceId: "inst-local", PlatformId: 1}}}}
	remote := &stubRemotePresenceReader{result: map[string][]RouteConnRef{"u1": {{UserId: "u1", ConnId: "c2", InstanceId: "inst-remote", PlatformId: 5}}}}
	svc := NewPresenceService(local.SnapshotOnlineUsers, remote.GetUsersPresenceConnRefs, "inst-local")

	results, meta, err := svc.GetUsersOnlineStatus(context.Background(), []string{"u1"})
	require.NoError(t, err)
	require.Nil(t, meta)
	require.Len(t, results, 1)
	require.Equal(t, constant.StatusOnline, results[0].Status)
	require.Len(t, results[0].DetailPlatformStatus, 2)
}

func TestGetUsersOnlineStatusReturnsPartialMetaWhenRemoteFails(t *testing.T) {
	local := &stubLocalPresenceReader{users: map[string][]RouteConnRef{"u1": {{UserId: "u1", ConnId: "c1", InstanceId: "inst-local", PlatformId: 1}}}}
	remote := &stubRemotePresenceReader{err: errors.New("boom")}
	svc := NewPresenceService(local.SnapshotOnlineUsers, remote.GetUsersPresenceConnRefs, "inst-local")

	results, meta, err := svc.GetUsersOnlineStatus(context.Background(), []string{"u1"})
	require.NoError(t, err)
	require.Equal(t, response.Meta{"partial": true, "data_source": "local_only", "reason": "remote_presence_lookup_failed"}, meta)
	require.Len(t, results, 1)
	require.Equal(t, constant.StatusOnline, results[0].Status)
}

func TestGetUsersOnlineStatusDoesNotTreatRouteableFalseAsOffline(t *testing.T) {
	local := &stubLocalPresenceReader{users: map[string][]RouteConnRef{}}
	remote := &stubRemotePresenceReader{result: map[string][]RouteConnRef{"u1": {{UserId: "u1", ConnId: "c2", InstanceId: "inst-remote", PlatformId: 5}}}}
	svc := NewPresenceService(local.SnapshotOnlineUsers, remote.GetUsersPresenceConnRefs, "inst-local")

	results, meta, err := svc.GetUsersOnlineStatus(context.Background(), []string{"u1"})
	require.NoError(t, err)
	require.Nil(t, meta)
	require.Equal(t, constant.StatusOnline, results[0].Status)
}

func TestGetUsersOnlineStatusKeepsPlatformDetailShape(t *testing.T) {
	local := &stubLocalPresenceReader{users: map[string][]RouteConnRef{"u1": {{UserId: "u1", ConnId: "c1", InstanceId: "inst-local", PlatformId: 1}}}}
	remote := &stubRemotePresenceReader{}
	svc := NewPresenceService(local.SnapshotOnlineUsers, remote.GetUsersPresenceConnRefs, "inst-local")

	results, _, err := svc.GetUsersOnlineStatus(context.Background(), []string{"u1"})
	require.NoError(t, err)
	require.Len(t, results[0].DetailPlatformStatus, 1)
	require.Equal(t, 1, results[0].DetailPlatformStatus[0].PlatformId)
	require.Equal(t, "iOS", results[0].DetailPlatformStatus[0].PlatformName)
	require.Equal(t, "c1", results[0].DetailPlatformStatus[0].ConnId)
}
