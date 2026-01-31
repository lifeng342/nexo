# OpenIM vs Nexo IM Interface and Logic Comparison

## Scope
- Compared Nexo endpoints in `internal/router/router.go` with their handlers/services/repos/gateway implementations.
- Used the local `open-im-server/` codebase as the OpenIM baseline (API, msggateway, and msg RPC flows).

## Interface alignment (major endpoints)
- Auth: Nexo `/auth/register`, `/auth/login` vs OpenIM `/auth/get_user_token`, `/auth/parse_token`, `/auth/force_logout` (OpenIM has token lifecycle + admin tokens; Nexo only issues JWT).
- User: Nexo `/user/info`, `/user/update` vs OpenIM `/user/user_register`, `/user/update_user_info_ex`, `/user/get_users_info`, `/user/get_users_online_status` (OpenIM has public/private fields, online status, rate limiting).
- Group: Nexo `/group/create|join|quit|info|members` vs OpenIM `/group/create_group|join_group|quit_group|...` (OpenIM has application/approval, invites, roles, mute, dismiss).
- Message: Nexo `/msg/send|pull|max_seq` + WS `WSSendMsg/WSPullMsg/WSGetNewestSeq` vs OpenIM msggateway + `/msg/*` APIs (OpenIM validates friends/blacklists, receipts, revoke, offline push).
- Conversation: Nexo `/conversation/list|info|update|mark_read|max_read_seq|unread_count` vs OpenIM `/conversation/*` + msg `GetConversationsHasReadAndMaxSeq`.

## Core logic differences and risks
- High: Single chat conversations are created without `peer_user_id` and without `seq_users` rows, so unread counts and peer display are wrong and sender read_seq does not persist. Files: `internal/service/message_service.go`, `internal/repository/conversation_repo.go`, `internal/repository/seq_repo.go`.
- High: Group join does not reset `seq_users.max_seq` on rejoin, so users who left and rejoin cannot see new messages (max_seq clamp). Files: `internal/service/group_service.go`, `internal/repository/seq_repo.go`.
- High: `GetMaxSeq` and conversation seq APIs allow any authenticated user to query arbitrary `conversation_id` without membership checks (information leak). Files: `internal/handler/message_handler.go`, `internal/handler/conversation_handler.go`, `internal/service/message_service.go`, `internal/service/conversation_service.go`.
- Medium: Group conversation records are never created for members (no `conversations` rows for group chats), so group chats may be missing from `/conversation/list` and updated_at is not bumped on new messages. Files: `internal/service/group_service.go`, `internal/service/message_service.go`, `internal/repository/conversation_repo.go`.
- Medium: Concurrent idempotent sends with same `client_msg_id` can race and return errors instead of the existing message (duplicate key handling missing). Files: `internal/service/message_service.go`, `internal/repository/message_repo.go`.
- Medium: Message send does not verify that `recv_id` exists (or that sender/receiver are allowed to chat). OpenIM enforces friend/blacklist checks; this is open. File: `internal/service/message_service.go`.
- Medium: `PullMessages` HTTP handler builds `MessageInfo` but returns raw `Message` list; response shape diverges from WebSocket and design doc. File: `internal/handler/message_handler.go`.
- Low: Redis seq is not initialized from MySQL on startup; if Redis is flushed, seq can reset and break inserts due to duplicate `conversation_id+seq`. Files: `cmd/server/main.go`, `internal/repository/seq_repo.go`.
- Low: Group member list/info is readable by any authenticated user; OpenIM restricts to members/admins. Files: `internal/handler/group_handler.go`, `internal/service/group_service.go`.
- Low: CORS config uses `Access-Control-Allow-Origin: *` with credentials, which browsers will ignore; consider config-based origins. File: `internal/middleware/cors.go`.

## Fix recommendations (targeted)
- For single chat, create `seq_users` rows and proper `peer_user_id` for both participants; update `EnsureConversationsExist` to accept per-user peer IDs or build the records directly in `SendSingleMessage`.
- On group create/join, create conversation rows for the creator and new members; on group message send, bump `updated_at` for all members (or at least sender + active members).
- On group rejoin, clear `seq_users.max_seq` to 0 (or set to joinSeq-1) so pulls are not clamped by the old leave bound.
- Add access checks before returning seq/unread APIs; prefer checking conversation ownership or membership (reuse `checkConversationAccess`).
- Handle duplicate key errors on message insert by re-querying `(sender_id, client_msg_id)` and returning the existing message.
- Validate `recv_id` exists and optionally enforce friend/blacklist rules to match OpenIM behavior.
- Initialize Redis seq on startup from MySQL (or on first send) to prevent seq regression.
- Align HTTP `pull` response with `MessageInfo` payload for consistency with WS.

---

## Detailed Module Comparison

### 1. WebSocket Gateway

| Feature | OpenIM | Nexo | Risk |
|---------|--------|------|------|
| Connection Management | gorilla/websocket with separate read/write goroutines | hertz-contrib/websocket with similar pattern | Low |
| Write Buffer | 256 capacity channel | 256 capacity channel | OK |
| Ping/Pong | Configurable pingPeriod/pongWait | Configurable pingPeriod/pongWait | OK |
| Write Timeout | SetWriteDeadline before each write | SetWriteDeadline before each write | OK |
| Slow Consumer | Returns error on channel full | Returns ErrWriteChannelFull | OK |
| Message Compression | Supports compression negotiation | Not implemented | Low |
| Multi-platform Kick | Kicks old connection on same platform | Kicks old connection on same platform | OK |

**Nexo Gateway Strengths:**
- Clean separation of read/write goroutines
- Proper use of sync.Once for close handling
- Atomic bool for closed state

**Missing in Nexo:**
- Message compression support (optional)
- Connection metrics/monitoring hooks

### 2. Group Management

| Feature | OpenIM | Nexo | Risk |
|---------|--------|------|------|
| Group ID Generation | MD5(owner+members+timestamp) | UUID (now sonyflake) | Fixed |
| Group Types | Normal, Super, Working | Single type | Low |
| Member Roles | Owner, Admin, Member | Owner, Admin, Member | OK |
| Join Approval | Application/Approval workflow | Direct join | Medium |
| Invite Members | Supported with approval | Not implemented | Medium |
| Mute Members | Supported | Not implemented | Low |
| Dismiss Group | Supported with cleanup | Not implemented | Medium |
| Webhook Callbacks | Full lifecycle callbacks | Not implemented | Medium |
| Max Members | Configurable per group type | No limit | Low |

**Critical Issues:**
1. **Rejoin seq not reset** - Users who leave and rejoin cannot see messages sent while they were gone
2. **No conversation records for members** - Group chats missing from conversation list
3. **No membership validation** - Any user can query group info

**Recommended Fixes:**

```go
// In group_service.go JoinGroup:
func (s *GroupService) JoinGroup(ctx context.Context, groupId, userId string) error {
    // ... existing join logic ...

    // Reset seq_users.max_seq on rejoin
    if err := s.seqRepo.ResetUserMaxSeq(ctx, conversationId, userId); err != nil {
        return err
    }

    // Create conversation record for new member
    conv := &model.Conversation{
        ConversationId:   conversationId,
        UserId:           userId,
        ConversationType: model.ConversationTypeGroup,
        GroupId:          groupId,
    }
    if err := s.convRepo.CreateConversation(ctx, conv); err != nil {
        // Ignore duplicate key error
    }

    return nil
}
```

### 3. Message Handling

| Feature | OpenIM | Nexo | Risk |
|---------|--------|------|------|
| Message ID | ServerMsgID (UUID) + ClientMsgID | server_msg_id (UUID) + client_msg_id | OK |
| Sequence Number | Per-conversation atomic increment | Redis INCR per conversation | OK |
| Idempotency | Checks client_msg_id before insert | No duplicate handling | High |
| Friend/Blacklist Check | Validates before send | No validation | Medium |
| Offline Storage | MongoDB with TTL | MySQL messages table | OK |
| Read Receipts | Supported | Supported via mark_read | OK |
| Message Revoke | Supported | Not implemented | Low |
| Message Reactions | Not in core | Not implemented | Low |
| Push Notification | Integrated offline push | Not implemented | Medium |

**Critical Issues:**
1. **No idempotency** - Duplicate sends can create duplicate messages
2. **No receiver validation** - Can send to non-existent users
3. **Single chat peer_user_id missing** - Conversation display broken

**Recommended Fixes:**

```go
// In message_repo.go - Add idempotent insert:
func (r *MessageRepo) CreateMessageIdempotent(ctx context.Context, msg *model.Message) (*model.Message, error) {
    err := r.db.WithContext(ctx).Create(msg).Error
    if err != nil {
        // Check for duplicate key error
        if isDuplicateKeyError(err) {
            // Return existing message
            var existing model.Message
            if err := r.db.WithContext(ctx).
                Where("sender_id = ? AND client_msg_id = ?", msg.SenderId, msg.ClientMsgId).
                First(&existing).Error; err != nil {
                return nil, err
            }
            return &existing, nil
        }
        return nil, err
    }
    return msg, nil
}
```

### 4. Online Status Management

| Feature | OpenIM | Nexo | Risk |
|---------|--------|------|------|
| Status Storage | Redis hash per user | Redis hash per user | OK |
| Multi-platform | Tracks each platform separately | Tracks each platform separately | OK |
| Heartbeat | WebSocket ping/pong | WebSocket ping/pong | OK |
| Status Broadcast | Publishes to subscribers | Not implemented | Medium |
| Batch Query | GetUsersOnlineStatus API | Not implemented | Low |
| Background/Foreground | Tracks app state | Not implemented | Low |

**Missing Features:**
- Status change notifications to friends/subscribers
- Batch online status query API
- App foreground/background state tracking

### 5. Conversation Management

| Feature | OpenIM | Nexo | Risk |
|---------|--------|------|------|
| Conversation Creation | Auto-created on first message | Manual creation required | Medium |
| Unread Count | Calculated from seq difference | Calculated from seq difference | OK |
| Pin Conversation | Supported | Supported | OK |
| Mute Conversation | Supported | Not implemented | Low |
| Draft | Supported | Not implemented | Low |
| Private Chat | Burn after read | Not implemented | Low |

---

## Security Audit

### High Priority
1. **Information Disclosure** - Seq/unread APIs lack membership checks
   - File: `internal/handler/conversation_handler.go`
   - Fix: Add `checkConversationAccess` before returning data

2. **Missing Input Validation** - recv_id not validated
   - File: `internal/service/message_service.go`
   - Fix: Verify user exists before sending

### Medium Priority
3. **CORS Misconfiguration** - `Access-Control-Allow-Origin: *` with credentials
   - File: `internal/middleware/cors.go`
   - Fix: Use configurable allowed origins

4. **No Rate Limiting** - Message send has no rate limit
   - Fix: Add Redis-based rate limiter

### Low Priority
5. **Token Storage** - JWT tokens not stored in Redis for revocation
   - Fix: Store tokens in Redis with TTL (Task #1)

---

## Implementation Priority

### Phase 1 - Critical Fixes (Data Integrity)
1. Fix single chat conversation creation with peer_user_id
2. Add seq_users reset on group rejoin
3. Implement idempotent message insert
4. Add conversation access checks

### Phase 2 - Security Hardening
5. Add receiver validation before message send
6. Fix CORS configuration
7. Implement token Redis storage (Task #1)
8. Add rate limiting

### Phase 3 - Feature Parity
9. Add group dismiss functionality
10. Implement offline push notifications
11. Add message revoke
12. Implement status broadcast

---

## Code Quality Observations

**Nexo Strengths:**
- Clean architecture with clear separation (handler/service/repo)
- Good use of context for request tracing
- Proper error handling with custom error types
- Well-structured WebSocket gateway

**Areas for Improvement:**
- Add more comprehensive unit tests
- Implement integration tests for critical flows
- Add OpenTelemetry tracing
- Consider adding circuit breakers for external calls
