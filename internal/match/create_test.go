package match

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nglong14/CodeDuel/internal/redisx"
)

func TestCreateMatchValidatesBeforeBeginning(t *testing.T) {
	players := testPlayers()
	tests := []struct {
		name     string
		duration time.Duration
		players  [2]redisx.QueueMember
		want     string
	}{
		{"zero duration", 0, players, "duration"},
		{"duplicate players", time.Minute, [2]redisx.QueueMember{players[0], players[0]}, "distinct"},
		{"missing database", time.Minute, players, "missing database"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := createMatch(context.Background(), nil, tt.duration, tt.players)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("createMatch error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func testPlayers() [2]redisx.QueueMember {
	firstID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	secondID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	return [2]redisx.QueueMember{
		{
			UserID:      firstID,
			PresenceKey: redisx.PresenceKey(firstID, uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")),
			Route:       redisx.UserChannel(firstID),
		},
		{
			UserID:      secondID,
			PresenceKey: redisx.PresenceKey(secondID, uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")),
			Route:       redisx.UserChannel(secondID),
		},
	}
}
