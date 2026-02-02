package entity

// User represents a user in the system
type User struct {
	Id        string  `json:"id" gorm:"column:id;primaryKey"`
	Nickname  string  `json:"nickname" gorm:"column:nickname"`
	Avatar    string  `json:"avatar" gorm:"column:avatar"`
	Password  string  `json:"-" gorm:"column:password"`
	Extra     *string `json:"extra" gorm:"column:extra;type:json"`
	CreatedAt int64   `json:"created_at" gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt int64   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime:milli"`
}

// TableName returns the table name for User
func (User) TableName() string {
	return "users"
}

// UserInfo represents public user info (without password)
type UserInfo struct {
	Id        string  `json:"id"`
	Nickname  string  `json:"nickname"`
	Avatar    string  `json:"avatar"`
	Extra     *string `json:"extra,omitempty"`
	CreatedAt int64   `json:"created_at"`
}

// ToUserInfo converts User to UserInfo
func (u *User) ToUserInfo() *UserInfo {
	return &UserInfo{
		Id:        u.Id,
		Nickname:  u.Nickname,
		Avatar:    u.Avatar,
		Extra:     u.Extra,
		CreatedAt: u.CreatedAt,
	}
}
