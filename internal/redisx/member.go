package redisx

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type QueueMember struct {
	UserID      uuid.UUID `json:"user_id"`
	PresenceKey string    `json:"presence_key"`
	Route       string    `json:"route"`
	Rating      int       `json:"rating"`
}

func (m QueueMember) Validate() error {
	if m.UserID == uuid.Nil {
		return fmt.Errorf("queue member: missing user ID")
	}
	if strings.TrimSpace(m.PresenceKey) == "" {
		return fmt.Errorf("queue member: missing presence key")
	}
	if strings.TrimSpace(m.Route) == "" {
		return fmt.Errorf("queue member: missing route")
	}
	if m.Route != UserChannel(m.UserID) {
		return fmt.Errorf("queue member: route does not match user")
	}

	prefix := presencePrefix + m.UserID.String() + ":"
	if !strings.HasPrefix(m.PresenceKey, prefix) {
		return fmt.Errorf("queue member: presence key does not match user")
	}
	connectionID, err := uuid.Parse(strings.TrimPrefix(m.PresenceKey, prefix))
	if err != nil || connectionID == uuid.Nil {
		return fmt.Errorf("queue member: invalid connection ID")
	}
	return nil
}

func encodeMember(member QueueMember) (string, error) {
	if err := member.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(member)
	if err != nil {
		return "", fmt.Errorf("encode queue member: %w", err)
	}
	return string(raw), nil
}

func decodeMember(raw string) (QueueMember, error) {
	var member QueueMember
	if err := json.Unmarshal([]byte(raw), &member); err != nil {
		return QueueMember{}, fmt.Errorf("decode queue member: %w", err)
	}
	if err := member.Validate(); err != nil {
		return QueueMember{}, err
	}
	return member, nil
}
