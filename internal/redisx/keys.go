package redisx

import "github.com/google/uuid"

const (
	QueueKey     = "codeduel:matchmaking:queue"
	MembersKey   = "codeduel:matchmaking:members"
	LastScoreKey = "codeduel:matchmaking:last-score"

	presencePrefix = "codeduel:presence:"
	userPrefix     = "codeduel:user:"
)

func PresenceKey(userID, connectionID uuid.UUID) string {
	return presencePrefix + userID.String() + ":" + connectionID.String()
}

func UserChannel(userID uuid.UUID) string {
	return userPrefix + userID.String()
}
