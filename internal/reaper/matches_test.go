package reaper

import (
	"testing"

	"github.com/google/uuid"
)

func TestDecideTiebreakWinner(t *testing.T) {
	playerOne := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	playerTwo := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	players := [2]uuid.UUID{playerOne, playerTwo}

	tests := []struct {
		name      string
		scores    [2]int
		wantValid bool
		want      uuid.UUID
	}{
		{name: "player one higher", scores: [2]int{2, 1}, wantValid: true, want: playerOne},
		{name: "player two higher", scores: [2]int{1, 3}, wantValid: true, want: playerTwo},
		{name: "equal nonzero draw", scores: [2]int{2, 2}},
		{name: "equal zero draw", scores: [2]int{0, 0}},
		{name: "one vs zero", scores: [2]int{1, 0}, wantValid: true, want: playerOne},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := decideTiebreakWinner(players, test.scores)
			if got.Valid != test.wantValid {
				t.Fatalf("winner valid = %v, want %v (%s)", got.Valid, test.wantValid, got.UUID)
			}
			if test.wantValid && got.UUID != test.want {
				t.Fatalf("winner = %s, want %s", got.UUID, test.want)
			}
			reversed := decideTiebreakWinner([2]uuid.UUID{playerTwo, playerOne}, [2]int{test.scores[1], test.scores[0]})
			if reversed.Valid != test.wantValid {
				t.Fatalf("reversed valid = %v, want %v", reversed.Valid, test.wantValid)
			}
			if test.wantValid && reversed.UUID != test.want {
				t.Fatalf("reversed winner = %s, want %s", reversed.UUID, test.want)
			}
		})
	}
}
