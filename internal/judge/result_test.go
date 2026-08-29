package judge

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nglong14/CodeDuel/internal/proto"
)

func TestTerminalResultFromOutcome(t *testing.T) {
	tests := []struct {
		name    string
		outcome ExecutionOutcome
		want    terminalResult
	}{
		{"pass", ExecutionOutcome{Kind: OutcomePass, TestsPassed: 3}, terminalResult{Verdict: proto.VerdictPass, TestsPassed: 3}},
		{"wrong answer", ExecutionOutcome{Kind: OutcomeWrongAnswer, TestsPassed: 2}, terminalResult{Verdict: proto.VerdictFail, FailureKind: "wrong_answer", TestsPassed: 2}},
		{"compile error", ExecutionOutcome{Kind: OutcomeCompileError}, terminalResult{Verdict: proto.VerdictError, FailureKind: "compile_error"}},
		{"runtime error", ExecutionOutcome{Kind: OutcomeRuntimeError, TestsPassed: 1}, terminalResult{Verdict: proto.VerdictError, FailureKind: "runtime_error", TestsPassed: 1}},
		{"output limit", ExecutionOutcome{Kind: OutcomeOutputLimit, TestsPassed: 1}, terminalResult{Verdict: proto.VerdictError, FailureKind: "output_limit", TestsPassed: 1}},
		{"timeout", ExecutionOutcome{Kind: OutcomeTimeout, TestsPassed: 1}, terminalResult{Verdict: proto.VerdictTimeout, TestsPassed: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := terminalResultFromOutcome(test.outcome, 3)
			if err != nil {
				t.Fatalf("terminalResultFromOutcome: %v", err)
			}
			if got != test.want {
				t.Fatalf("result = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestStableResultEventID(t *testing.T) {
	submissionID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	recipientID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	want := uuid.MustParse("6e12cabe-af50-57d0-a645-3ca3780bea5d")

	first := stableResultEventID(resultKindSubmission, submissionID, recipientID)
	second := stableResultEventID(resultKindSubmission, submissionID, recipientID)
	if first != want || second != want {
		t.Fatalf("event IDs = (%s, %s), want %s", first, second, want)
	}
}

func TestBuildResultEventsWithoutWinner(t *testing.T) {
	completed := testCompletedSubmission()
	events, err := buildResultEvents(completed)
	if err != nil {
		t.Fatalf("buildResultEvents: %v", err)
	}
	if len(events) != 1 || events[0].RecipientID != completed.PlayerID {
		t.Fatalf("events = %#v, want one event for %s", events, completed.PlayerID)
	}

	data := decodeResultEvent(t, events[0].Payload)
	want := proto.ResultData{
		EventID:      "6e12cabe-af50-57d0-a645-3ca3780bea5d",
		SubmissionID: completed.SubmissionID.String(),
		MatchID:      completed.MatchID.String(),
		PlayerID:     completed.PlayerID.String(),
		Verdict:      proto.VerdictFail,
		TestsPassed:  2,
		TotalTests:   3,
	}
	if data != want {
		t.Fatalf("result data = %#v, want %#v", data, want)
	}
}

func TestBuildResultEventsWithWinner(t *testing.T) {
	completed := testCompletedSubmission()
	completed.Verdict = proto.VerdictPass
	completed.TestsPassed = completed.TotalTests
	completed.WinnerID = completed.Players[0]

	events, err := buildResultEvents(completed)
	if err != nil {
		t.Fatalf("buildResultEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	wantIDs := []string{
		"0854737d-2cf8-5eed-b4ae-ac066aeca86d",
		"9cdf1b1c-9203-5256-a6a1-64c4ec5b7ff7",
	}
	wantOutcomes := []string{proto.OutcomeWin, proto.OutcomeLoss}
	for index, event := range events {
		if event.RecipientID != completed.Players[index] {
			t.Fatalf("recipient %d = %s, want %s", index, event.RecipientID, completed.Players[index])
		}
		data := decodeResultEvent(t, event.Payload)
		if data.EventID != wantIDs[index] || data.WinnerID != completed.WinnerID.String() || data.Outcome != wantOutcomes[index] {
			t.Fatalf("result data %d = %#v", index, data)
		}
		if data.SubmissionID != completed.SubmissionID.String() || data.MatchID != completed.MatchID.String() ||
			data.PlayerID != completed.PlayerID.String() || data.Verdict != proto.VerdictPass ||
			data.TestsPassed != completed.TotalTests || data.TotalTests != completed.TotalTests {
			t.Fatalf("common result data %d = %#v", index, data)
		}
	}
}

func decodeResultEvent(t *testing.T, payload []byte) proto.ResultData {
	t.Helper()
	envelope, err := proto.Decode(payload)
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
	return data
}

func testCompletedSubmission() completedSubmission {
	playerOne := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	playerTwo := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	return completedSubmission{
		SubmissionID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		MatchID:      uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		PlayerID:     playerOne,
		Players:      [2]uuid.UUID{playerOne, playerTwo},
		Verdict:      proto.VerdictFail,
		FailureKind:  "wrong_answer",
		TestsPassed:  2,
		TotalTests:   3,
	}
}
