package service

import (
	"context"

	"github.com/mbeoliero/kit/log"
	"github.com/mbeoliero/nexo/internal/entity"
	"github.com/mbeoliero/nexo/internal/repository"
	"github.com/mbeoliero/nexo/pkg/errcode"
)

// ConversationService handles conversation-related business logic
type ConversationService struct {
	convRepo *repository.ConversationRepo
	seqRepo  *repository.SeqRepo
	repos    *repository.Repositories
}

// NewConversationService creates a new ConversationService
func NewConversationService(repos *repository.Repositories) *ConversationService {
	return &ConversationService{
		convRepo: repos.Conversation,
		seqRepo:  repos.Seq,
		repos:    repos,
	}
}

// GetUserConversations gets all conversations for a user
func (s *ConversationService) GetUserConversations(ctx context.Context, userId string) ([]*entity.ConversationInfo, error) {
	convWithSeqs, err := s.convRepo.GetUserConversationsWithSeq(ctx, userId)
	if err != nil {
		log.CtxError(ctx, "get user conversations failed: user_id=%s, error=%v", userId, err)
		return nil, errcode.ErrInternalServer
	}

	result := make([]*entity.ConversationInfo, 0, len(convWithSeqs))
	for _, conv := range convWithSeqs {
		info := &entity.ConversationInfo{
			ConversationId:   conv.ConversationId,
			ConversationType: conv.ConversationType,
			PeerUserId:       conv.PeerUserId,
			GroupId:          conv.GroupId,
			RecvMsgOpt:       conv.RecvMsgOpt,
			IsPinned:         conv.IsPinned,
			UnreadCount:      conv.UnreadCount,
			MaxSeq:           conv.MaxSeq,
			ReadSeq:          conv.ReadSeq,
			UpdatedAt:        conv.UpdatedAt,
		}
		result = append(result, info)
	}

	return result, nil
}

// GetConversation gets a specific conversation for a user
func (s *ConversationService) GetConversation(ctx context.Context, userId, conversationId string) (*entity.ConversationInfo, error) {
	conv, err := s.convRepo.GetByOwnerAndConvId(ctx, userId, conversationId)
	if err != nil {
		log.CtxError(ctx, "get conversation failed: user_id=%s, conversation_id=%s, error=%v", userId, conversationId, err)
		return nil, errcode.ErrInternalServer
	}
	if conv == nil {
		return nil, errcode.ErrConvNotFound
	}

	// Get seq info
	seqConv, _ := s.seqRepo.GetConversationSeqInfo(ctx, conversationId)
	seqUser, _ := s.seqRepo.GetSeqUser(ctx, userId, conversationId)

	maxSeq := int64(0)
	readSeq := int64(0)
	if seqConv != nil {
		maxSeq = seqConv.MaxSeq
	}
	if seqUser != nil {
		readSeq = seqUser.ReadSeq
	}

	unreadCount := maxSeq - readSeq
	if unreadCount < 0 {
		unreadCount = 0
	}

	return &entity.ConversationInfo{
		ConversationId:   conv.ConversationId,
		ConversationType: conv.ConversationType,
		PeerUserId:       conv.PeerUserId,
		GroupId:          conv.GroupId,
		RecvMsgOpt:       conv.RecvMsgOpt,
		IsPinned:         conv.IsPinned,
		UnreadCount:      unreadCount,
		MaxSeq:           maxSeq,
		ReadSeq:          readSeq,
		UpdatedAt:        conv.UpdatedAt,
	}, nil
}

// UpdateConversationRequest represents update conversation request
type UpdateConversationRequest struct {
	RecvMsgOpt *int32 `json:"recv_msg_opt,omitempty"`
	IsPinned   *bool  `json:"is_pinned,omitempty"`
}

// UpdateConversation updates conversation settings
func (s *ConversationService) UpdateConversation(ctx context.Context, userId, conversationId string, req *UpdateConversationRequest) error {
	updates := make(map[string]interface{})
	if req.RecvMsgOpt != nil {
		updates["recv_msg_opt"] = *req.RecvMsgOpt
	}
	if req.IsPinned != nil {
		updates["is_pinned"] = *req.IsPinned
	}

	if len(updates) == 0 {
		return nil
	}

	if err := s.convRepo.Update(ctx, userId, conversationId, updates); err != nil {
		log.CtxError(ctx, "update conversation failed: %v", err)
		return errcode.ErrInternalServer
	}

	return nil
}

// MarkRead marks a conversation as read up to a seq
func (s *ConversationService) MarkRead(ctx context.Context, userId, conversationId string, readSeq int64) error {
	if err := s.seqRepo.UpdateReadSeq(ctx, userId, conversationId, readSeq); err != nil {
		log.CtxError(ctx, "update read seq failed: %v", err)
		return errcode.ErrInternalServer
	}
	return nil
}

// GetMaxReadSeq gets the max seq and read seq for a conversation
func (s *ConversationService) GetMaxReadSeq(ctx context.Context, userId, conversationId string) (maxSeq, readSeq int64, err error) {
	seqConv, err := s.seqRepo.GetConversationSeqInfo(ctx, conversationId)
	if err != nil {
		return 0, 0, err
	}

	seqUser, _ := s.seqRepo.GetSeqUser(ctx, userId, conversationId)
	if seqUser != nil {
		readSeq = seqUser.ReadSeq
	}

	return seqConv.MaxSeq, readSeq, nil
}
