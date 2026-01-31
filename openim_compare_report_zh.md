# OpenIM 与 Nexo IM 接口与核心逻辑对比（中文）

## 范围
- 对比 Nexo 的 HTTP/WS 路由、Handler/Service/Repo/Gateway 实现。
- 以本地 `open-im-server/` 代码作为 OpenIM 基线（API、msggateway、msg RPC 流程）。

## 接口对齐（主要端点）
- 鉴权：Nexo `/auth/register`、`/auth/login`；OpenIM `/auth/get_user_token`、`/auth/parse_token`、`/auth/force_logout`（OpenIM 有 token 生命周期与管理员 token）。
- 用户：Nexo `/user/info`、`/user/update`；OpenIM `/user/user_register`、`/user/update_user_info_ex`、`/user/get_users_info`、`/user/get_users_online_status`（OpenIM 覆盖在线状态与更多用户维度）。
- 群组：Nexo `/group/create|join|quit|info|members`；OpenIM `/group/create_group|join_group|quit_group|...`（OpenIM 包含申请/审批/邀请/转让/禁言/解散等）。
- 消息：Nexo `/msg/send|pull|max_seq` + WS `WSSendMsg/WSPullMsg/WSGetNewestSeq`；OpenIM 通过 msggateway + `/msg/*`，包含黑名单/好友校验、回执、撤回、离线推送等。
- 会话：Nexo `/conversation/list|info|update|mark_read|max_read_seq|unread_count`；OpenIM `/conversation/*` + msg `GetConversationsHasReadAndMaxSeq`。

## 核心逻辑差异与风险
- 高：单聊会话未为双方建立 `peer_user_id` 和 `seq_users`，导致未读数不准确、对端信息缺失、read_seq 不落库。文件：`internal/service/message_service.go`、`internal/repository/conversation_repo.go`、`internal/repository/seq_repo.go`。
- 高：退群后再入群未重置 `seq_users.max_seq`，历史离群上限会继续截断新消息，导致“重新入群看不到新消息”。文件：`internal/service/group_service.go`、`internal/repository/seq_repo.go`。
- 高：`GetMaxSeq` 与会话未读相关接口未做会话归属校验，任意登录用户可查询任意 `conversation_id`（信息泄漏）。文件：`internal/handler/message_handler.go`、`internal/handler/conversation_handler.go`、`internal/service/message_service.go`、`internal/service/conversation_service.go`。
- 中：群会话未为成员创建 `conversations` 记录，群聊不一定出现在 `/conversation/list`，且消息发送不会更新会话 `updated_at`。文件：`internal/service/group_service.go`、`internal/service/message_service.go`、`internal/repository/conversation_repo.go`。
- 中：并发重试下 `client_msg_id` 幂等存在竞态，唯一键冲突后不会回查既有消息。文件：`internal/service/message_service.go`、`internal/repository/message_repo.go`。
- 中：单聊不校验 `recv_id` 是否存在，也不做黑名单/好友关系校验，弱化权限控制（OpenIM 有相关校验）。文件：`internal/service/message_service.go`。
- 中：HTTP `pull` 响应内部构建了 `MessageInfo`，但实际返回的是 `Message` 原表结构，响应不一致。文件：`internal/handler/message_handler.go`。
- 低：未在启动时从 MySQL 初始化 Redis seq，Redis 丢失后可能导致 seq 回退并触发唯一键冲突。文件：`cmd/server/main.go`、`internal/repository/seq_repo.go`。
- 低：群成员信息/列表对任意登录用户可见，OpenIM 通常限制为成员/管理员。文件：`internal/handler/group_handler.go`、`internal/service/group_service.go`。
- 低：CORS 使用 `Access-Control-Allow-Origin: *` + `Allow-Credentials: true`，浏览器会忽略，建议改成配置白名单。文件：`internal/middleware/cors.go`。

## 修复建议（重点）
- 单聊：为双方创建 `seq_users`，并正确设置 `peer_user_id`；建议调整 `EnsureConversationsExist` 支持“每个 owner 对应不同 peer”或在 `SendSingleMessage` 中直接写入两条会话记录。
- 群：创建群会话记录给创建者/新成员；发送群消息时批量更新会话 `updated_at`。
- 退群再入群：重置 `seq_users.max_seq`（设为 0 或 joinSeq-1），避免历史 max_seq 继续截断。
- 权限：在 `GetMaxSeq`、`GetMaxReadSeq`、`PullMessages` 等路径统一做会话归属/成员校验，可复用 `checkConversationAccess`。
- 幂等：插入消息遇到唯一键冲突时回查 `(sender_id, client_msg_id)` 并返回已有消息。
- 单聊权限：至少校验 `recv_id` 存在，必要时补齐黑名单/好友校验以贴近 OpenIM。
- seq 初始化：启动时把 MySQL seq 同步到 Redis，避免回退。
- HTTP/WS 响应一致性：`/msg/pull` 返回 `MessageInfo` 结构，和 WS 保持一致。
