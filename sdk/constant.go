package sdk

// Session types
const (
	SessionTypeSingle = 1 // Single chat
	SessionTypeGroup  = 2 // Group chat
)

// Message types
const (
	MsgTypeText   = 1
	MsgTypeImage  = 2
	MsgTypeVideo  = 3
	MsgTypeAudio  = 4
	MsgTypeFile   = 5
	MsgTypeCustom = 100
)

// Group status
const (
	GroupStatusNormal    = 0
	GroupStatusDismissed = 1
)

// Group member status
const (
	GroupMemberStatusNormal = 0 // Normal
	GroupMemberStatusLeft   = 1 // Left
	GroupMemberStatusKicked = 2 // Kicked
)

// Group member role levels
const (
	RoleLevelMember = 0
	RoleLevelAdmin  = 1
	RoleLevelOwner  = 2
)

// Online status
const (
	StatusOffline = 0
	StatusOnline  = 1
)

// Receive message options
const (
	RecvMsgOptNormal   = 0 // Normal receive
	RecvMsgOptNoNotify = 1 // No notification
	RecvMsgOptNotRecv  = 2 // Do not receive
)

// Platform Ids
const (
	PlatformIdUnknown = 0
	PlatformIdIOS     = 1
	PlatformIdAndroid = 2
	PlatformIdWindows = 3
	PlatformIdMacOS   = 4
	PlatformIdWeb     = 5
)

// PlatformIdToName converts platform Id to name
func PlatformIdToName(platformId int) string {
	switch platformId {
	case PlatformIdIOS:
		return "iOS"
	case PlatformIdAndroid:
		return "Android"
	case PlatformIdWindows:
		return "Windows"
	case PlatformIdMacOS:
		return "macOS"
	case PlatformIdWeb:
		return "Web"
	default:
		return "Unknown"
	}
}
