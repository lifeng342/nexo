package gateway

import "time"

// WebSocket protocol constants (aligned with open-im-server)
const (
	// Request identifiers
	WSGetNewestSeq      = 1001 // Get newest seq
	WSPullMsgBySeqList  = 1002 // Pull messages by seq list
	WSSendMsg           = 1003 // Send message
	WSPullMsg           = 1005 // Pull messages
	WSGetConvMaxReadSeq = 1006 // Get conversation max/read seq

	// Response identifiers
	WSPushMsg       = 2001 // Server push message
	WSKickOnlineMsg = 2002 // Kick user offline
	WSDataError     = 3001 // Data error
)

// WebSocket message types
const (
	MessageText   = 1
	MessageBinary = 2
)

// Timeout constants
const (
	// WriteWait is time allowed to write a message to the peer
	WriteWait = 10 * time.Second

	// PongWait is time allowed to read the next pong message from the peer
	PongWait = 30 * time.Second

	// PingPeriod is period between pings. Must be less than PongWait
	PingPeriod = (PongWait * 9) / 10

	// MaxMessageSize is maximum message size allowed from peer
	MaxMessageSize = 51200
)

// Query parameter keys
const (
	QueryToken       = "token"
	QuerySendId      = "send_id"
	QueryPlatformId  = "platform_id"
	QueryOperationId = "operation_id"
	QuerySDKType     = "sdk_type"
	QueryIsMsgResp   = "is_msg_resp"
)

// SDK types
const (
	SDKTypeGo = "go"
	SDKTypeJS = "js"
)
