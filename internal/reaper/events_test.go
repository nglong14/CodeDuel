package reaper

import (
	"testing"

	"github.com/google/uuid"

	"github.com/nglong14/CodeDuel/internal/proto"
)

func TestBuildFailedResultEvent(t *testing.T) {
	submissionID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	matchID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	playerID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	event, err := buildFailedResultEvent(submissionID, matchID, playerID, 3)
	if err != nil {
		t.Fatalf("buildFailedResultEvent: %v", err)
	}
	if event.RecipientID != playerID {
		t.Fatalf("recipient = %s, want %s", event.RecipientID, playerID)
	}

	envelope, err := proto.Decode(event.Payload)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if envelope.Type != proto.TypeResult {
		t.Fatalf("type = %q, want %q", envelope.Type, proto.TypeResult)
	}
	var data proto.ResultData
	if err := envelope.DecodeData(&data); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	wantID := proto.StableEventID(eventKindInfrastructureFailed, submissionID, playerID).String()
	if data.EventID != wantID || data.SubmissionID != submissionID.String() ||
		data.MatchID != matchID.String() || data.PlayerID != playerID.String() ||
		data.Verdict != proto.VerdictFailed || data.TestsPassed != 0 || data.TotalTests != 3 ||
		data.WinnerID != "" || data.Outcome != "" {
		t.Fatalf("failed result = %#v", data)
	}

	again, err := buildFailedResultEvent(submissionID, matchID, playerID, 3)
	if err != nil {
		t.Fatalf("buildFailedResultEvent second: %v", err)
	}
	if string(again.Payload) != string(event.Payload) {
		t.Fatal("failed result event is not stable")
	}
}

func TestBuildMatchEndEvents(t *testing.T) {
	matchID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	playerOne := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	playerTwo := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	players := [2]uuid.UUID{playerOne, playerTwo}

	t.Run("winner", func(t *testing.T) {
		events, err := buildMatchEndEvents(matchID, players, [2]int{2, 1}, 3, uuid.NullUUID{UUID: playerOne, Valid: true})
		if err != nil {
			t.Fatalf("buildMatchEndEvents: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("event count = %d, want 2", len(events))
		}
		wantOutcomes := []string{proto.OutcomeWin, proto.OutcomeLoss}
		wantScores := []int{2, 1}
		for index, event := range events {
			if event.RecipientID != players[index] {
				t.Fatalf("recipient %d = %s, want %s", index, event.RecipientID, players[index])
			}
			data := decodeMatchEnd(t, event.Payload)
			if data.EventID != proto.StableEventID(eventKindMatchFinalized, matchID, players[index]).String() {
				t.Fatalf("event ID %d = %s", index, data.EventID)
			}
			if data.MatchID != matchID.String() || data.WinnerID != playerOne.String() ||
				data.Outcome != wantOutcomes[index] || data.TestsPassed != wantScores[index] || data.TotalTests != 3 {
				t.Fatalf("match end %d = %#v", index, data)
			}
		}
	})

	t.Run("draw", func(t *testing.T) {
		events, err := buildMatchEndEvents(matchID, players, [2]int{0, 0}, 3, uuid.NullUUID{})
		if err != nil {
			t.Fatalf("buildMatchEndEvents: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("event count = %d, want 2", len(events))
		}
		for index, event := range events {
			data := decodeMatchEnd(t, event.Payload)
			if data.WinnerID != "" || data.Outcome != proto.OutcomeDraw || data.TestsPassed != 0 || data.TotalTests != 3 {
				t.Fatalf("draw event %d = %#v", index, data)
			}
		}
	})
}

func decodeMatchEnd(t *testing.T, payload []byte) proto.MatchEndData {
	t.Helper()
	envelope, err := proto.Decode(payload)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if envelope.Type != proto.TypeMatchEnd {
		t.Fatalf("type = %q, want %q", envelope.Type, proto.TypeMatchEnd)
	}
	var data proto.MatchEndData
	if err := envelope.DecodeData(&data); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	return data
}
