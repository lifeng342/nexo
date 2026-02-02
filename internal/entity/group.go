package entity

import "github.com/mbeoliero/nexo/pkg/constant"

// Group represents a group
type Group struct {
	Id            string  `json:"id" gorm:"column:id;primaryKey"`
	Name          string  `json:"name" gorm:"column:name"`
	Introduction  string  `json:"introduction" gorm:"column:introduction"`
	Avatar        string  `json:"avatar" gorm:"column:avatar"`
	Extra         *string `json:"extra" gorm:"column:extra;type:json"`
	Status        int32   `json:"status" gorm:"column:status"`
	CreatorUserId string  `json:"creator_user_id" gorm:"column:creator_user_id"`
	GroupType     int32   `json:"group_type" gorm:"column:group_type"`
	CreatedAt     int64   `json:"created_at" gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt     int64   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime:milli"`
}

// TableName returns the table name for Group
func (Group) TableName() string {
	return "groups"
}

// IsNormal checks if group status is normal
func (g *Group) IsNormal() bool {
	return g.Status == constant.GroupStatusNormal
}

// GroupMember represents a group member
type GroupMember struct {
	Id            int64   `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	GroupId       string  `json:"group_id" gorm:"column:group_id"`
	UserId        string  `json:"user_id" gorm:"column:user_id"`
	GroupNickname string  `json:"group_nickname" gorm:"column:group_nickname"`
	GroupAvatar   string  `json:"group_avatar" gorm:"column:group_avatar"`
	Extra         *string `json:"extra" gorm:"column:extra;type:json"`
	RoleLevel     int32   `json:"role_level" gorm:"column:role_level"`
	Status        int32   `json:"status" gorm:"column:status"`
	JoinedAt      int64   `json:"joined_at" gorm:"column:joined_at"`
	JoinSeq       int64   `json:"join_seq" gorm:"column:join_seq"`
	InviterUserId string  `json:"inviter_user_id" gorm:"column:inviter_user_id"`
	CreatedAt     int64   `json:"created_at" gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt     int64   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime:milli"`
}

// TableName returns the table name for GroupMember
func (GroupMember) TableName() string {
	return "group_members"
}

// IsNormal checks if member status is normal
func (gm *GroupMember) IsNormal() bool {
	return gm.Status == constant.GroupMemberStatusNormal
}

// IsOwner checks if member is the group owner
func (gm *GroupMember) IsOwner() bool {
	return gm.RoleLevel == constant.RoleLevelOwner
}

// IsAdmin checks if member is an admin
func (gm *GroupMember) IsAdmin() bool {
	return gm.RoleLevel >= constant.RoleLevelAdmin
}

// GroupInfo represents group info with member count
type GroupInfo struct {
	Id            string `json:"id"`
	Name          string `json:"name"`
	Introduction  string `json:"introduction"`
	Avatar        string `json:"avatar"`
	Status        int32  `json:"status"`
	CreatorUserId string `json:"creator_user_id"`
	MemberCount   int64  `json:"member_count"`
	CreatedAt     int64  `json:"created_at"`
}

// GroupMemberInfo represents member info in group
type GroupMemberInfo struct {
	UserId        string `json:"user_id"`
	Nickname      string `json:"nickname"`
	Avatar        string `json:"avatar"`
	GroupNickname string `json:"group_nickname"`
	RoleLevel     int32  `json:"role_level"`
	JoinedAt      int64  `json:"joined_at"`
}
