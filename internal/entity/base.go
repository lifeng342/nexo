package entity

import (
	"fmt"
	"sort"
	"time"

	"github.com/mbeoliero/nexo/pkg/constant"
)

// NowUnixMilli returns current unix timestamp in milliseconds
func NowUnixMilli() int64 {
	return time.Now().UnixMilli()
}

// GenSingleConversationId generates conversation Id for single chat
// Format: si_{min(userA,userB)}:{max(userA,userB)}
// Uses ":" as separator between userIds to support userIds containing "_"
func GenSingleConversationId(userA, userB string) string {
	users := []string{userA, userB}
	sort.Strings(users)
	return fmt.Sprintf("%s%s:%s", constant.SingleConversationPrefix, users[0], users[1])
}

// GenGroupConversationId generates conversation Id for group chat
// Format: sg_{groupId}
func GenGroupConversationId(groupId string) string {
	return fmt.Sprintf("%s%s", constant.GroupConversationPrefix, groupId)
}

// IsSingleConversation checks if conversation Id is for single chat
func IsSingleConversation(conversationId string) bool {
	return len(conversationId) > 3 && conversationId[:3] == constant.SingleConversationPrefix
}

// IsGroupConversation checks if conversation Id is for group chat
func IsGroupConversation(conversationId string) bool {
	return len(conversationId) > 3 && conversationId[:3] == constant.GroupConversationPrefix
}
