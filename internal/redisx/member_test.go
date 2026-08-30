package redisx

import (
	"testing"

	"github.com/google/uuid"
)

func TestKeyHelpers(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	connectionID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	if got, want := UserChannel(userID), "codeduel:user:11111111-1111-1111-1111-111111111111"; got != want {
		t.Fatalf("UserChannel = %q, want %q", got, want)
	}
	if got, want := PresenceKey(userID, connectionID), "codeduel:presence:11111111-1111-1111-1111-111111111111:aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"; got != want {
		t.Fatalf("PresenceKey = %q, want %q", got, want)
	}
	if JudgeJobsKey != "codeduel:judge:jobs" || JudgeConsumerGroup != "codeduel:judges" {
		t.Fatalf("judge stream = %q, group = %q", JudgeJobsKey, JudgeConsumerGroup)
	}
}

func TestQueueMemberRoundTrip(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	member := QueueMember{
		UserID:      userID,
		PresenceKey: PresenceKey(userID, uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")),
		Route:       UserChannel(userID),
		Rating:      0,
	}
	encoded, err := encodeMember(member)
	if err != nil {
		t.Fatalf("encodeMember: %v", err)
	}
	decoded, err := decodeMember(encoded)
	if err != nil {
		t.Fatalf("decodeMember: %v", err)
	}
	if decoded != member {
		t.Fatalf("decoded = %#v, want %#v", decoded, member)
	}
}

func TestQueueMemberValidation(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	connectionID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	valid := QueueMember{
		UserID:      userID,
		PresenceKey: PresenceKey(userID, connectionID),
		Route:       UserChannel(userID),
	}

	tests := []struct {
		name   string
		mutate func(*QueueMember)
	}{
		{"missing user", func(m *QueueMember) { m.UserID = uuid.Nil }},
		{"wrong route", func(m *QueueMember) { m.Route = UserChannel(uuid.New()) }},
		{"wrong presence user", func(m *QueueMember) { m.PresenceKey = PresenceKey(uuid.New(), connectionID) }},
		{"invalid connection", func(m *QueueMember) { m.PresenceKey = presencePrefix + userID.String() + ":bad" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			member := valid
			tt.mutate(&member)
			if err := member.Validate(); err == nil {
				t.Fatal("Validate returned nil")
			}
		})
	}
}
