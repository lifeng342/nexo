package common

import (
	"fmt"
	"strconv"
)

const (
	PrefixLength = 4
)

// RoleType defines the actor role in the IM system.
type RoleType string

const (
	RoleUser  RoleType = "user"
	RoleAgent RoleType = "agent"
)

// Actor represents an external identity that maps to an IM user id.
type Actor struct {
	Id   int64
	Role RoleType
}

// ToIMUserId converts an Actor to the IM system's string user id.
//
//	Actor{ID: 42, Role: RoleUser}.ToIMUserId()  => "u___42"
//	Actor{ID: 7, Role: RoleAgent}.ToIMUserId()  => "ag__7"
func (a *Actor) ToIMUserId() (string, error) {
	switch a.Role {
	case RoleUser:
		return fmt.Sprintf("u___%d", a.Id), nil
	case RoleAgent:
		return fmt.Sprintf("ag__%d", a.Id), nil
	default:
		return "", fmt.Errorf("failed to transfer actor to user id, type: %s", a.Role)
	}
}

// FromIMUserId parses an IM user id string back into an Actor.
// Returns an error if the format is unrecognised.
func (a *Actor) FromIMUserId(userId string) error {
	if a == nil {
		return fmt.Errorf("actor is nil")
	}
	if len(userId) < PrefixLength+1 { // 至少要有 4 位前缀 + 1 位数字
		return fmt.Errorf("invalid userId: %q", userId)
	}
	prefix := userId[:4]
	idStr := userId[4:]
	switch prefix {
	case "u___":
		a.Role = RoleUser
	case "ag__":
		a.Role = RoleAgent
	default:
		return fmt.Errorf("unknown prefix: %q", prefix)
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid id: %q", idStr)
	}
	a.Id = id
	return nil
}
