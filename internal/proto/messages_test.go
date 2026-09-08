package proto

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEncodeDecodeJoinQueue(t *testing.T) {
	raw, err := Encode(TypeJoinQueue, JoinQueueData{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != TypeJoinQueue {
		t.Fatalf("type = %q, want %q", env.Type, TypeJoinQueue)
	}

	var data JoinQueueData
	if err := env.DecodeData(&data); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
}

func TestEncodeJoinQueueNilPayload(t *testing.T) {
	raw, err := Encode(TypeJoinQueue, nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != TypeJoinQueue {
		t.Fatalf("type = %q, want %q", env.Type, TypeJoinQueue)
	}
	if len(env.Data) != 0 {
		t.Fatalf("data = %s, want empty", env.Data)
	}
}

func TestEncodeDecodeLeaveQueue(t *testing.T) {
	raw, err := Encode(TypeLeaveQueue, LeaveQueueData{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != TypeLeaveQueue {
		t.Fatalf("type = %q, want %q", env.Type, TypeLeaveQueue)
	}
	var data LeaveQueueData
	if err := env.DecodeData(&data); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
}

func TestEncodeDecodeReady(t *testing.T) {
	want := ReadyData{UserID: "11111111-1111-1111-1111-111111111111"}
	raw, err := Encode(TypeReady, want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != TypeReady {
		t.Fatalf("type = %q, want %q", env.Type, TypeReady)
	}
	var got ReadyData
	if err := env.DecodeData(&got); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEncodeDecodeQueued(t *testing.T) {
	raw, err := Encode(TypeQueued, QueuedData{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != TypeQueued {
		t.Fatalf("type = %q, want %q", env.Type, TypeQueued)
	}
	if string(env.Data) != "{}" {
		t.Fatalf("data = %s, want {}", env.Data)
	}
}

func TestEncodeDecodeQueueLeft(t *testing.T) {
	want := QueueLeftData{Removed: true}
	raw, err := Encode(TypeQueueLeft, want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != TypeQueueLeft {
		t.Fatalf("type = %q, want %q", env.Type, TypeQueueLeft)
	}
	var got QueueLeftData
	if err := env.DecodeData(&got); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEncodeDecodeSubmitCode(t *testing.T) {
	want := SubmitCodeData{
		MatchID:   "11111111-1111-1111-1111-111111111111",
		RequestID: "22222222-2222-2222-2222-222222222222",
		Language:  "python",
		Code:      "print(1)",
	}
	raw, err := Encode(TypeSubmitCode, want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != TypeSubmitCode {
		t.Fatalf("type = %q, want %q", env.Type, TypeSubmitCode)
	}

	got, err := DecodeSubmitCodeData(env.Data)
	if err != nil {
		t.Fatalf("DecodeSubmitCodeData: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEncodeDecodeMatchStart(t *testing.T) {
	want := MatchStartData{
		MatchID:   "11111111-1111-1111-1111-111111111111",
		ProblemID: "22222222-2222-2222-2222-222222222222",
		Deadline:  time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC),
	}
	raw, err := Encode(TypeMatchStart, want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	var got MatchStartData
	if err := env.DecodeData(&got); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got.MatchID != want.MatchID || got.ProblemID != want.ProblemID {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !got.Deadline.Equal(want.Deadline) {
		t.Fatalf("deadline = %v, want %v", got.Deadline, want.Deadline)
	}
}

func TestEncodeDecodeJudging(t *testing.T) {
	want := JudgingData{
		RequestID:    "22222222-2222-2222-2222-222222222222",
		SubmissionID: "33333333-3333-3333-3333-333333333333",
	}
	raw, err := Encode(TypeJudging, want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	var got JudgingData
	if err := env.DecodeData(&got); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEncodeDecodeResult(t *testing.T) {
	failureKind := "wrong_answer"
	want := ResultData{
		EventID:      "55555555-5555-5555-5555-555555555555",
		RequestID:    "22222222-2222-2222-2222-222222222222",
		SubmissionID: "33333333-3333-3333-3333-333333333333",
		MatchID:      "11111111-1111-1111-1111-111111111111",
		PlayerID:     "22222222-2222-2222-2222-222222222222",
		Verdict:      VerdictPass,
		WinnerID:     "44444444-4444-4444-4444-444444444444",
		TestsPassed:  3,
		TotalTests:   3,
		Outcome:      "win",
		FailureKind:  &failureKind,
	}
	raw, err := Encode(TypeResult, want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	var got ResultData
	if err := env.DecodeData(&got); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got.FailureKind == nil || *got.FailureKind != failureKind {
		t.Fatalf("failure_kind = %#v, want %q", got.FailureKind, failureKind)
	}
	got.FailureKind = nil
	want.FailureKind = nil
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEncodeResultNilFailureKind(t *testing.T) {
	raw, err := Encode(TypeResult, ResultData{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.Contains(string(raw), `"failure_kind":null`) {
		t.Fatalf("result = %s, want failure_kind null", raw)
	}
}

func TestEncodeDecodeMatchEnd(t *testing.T) {
	want := MatchEndData{
		EventID:     "55555555-5555-5555-5555-555555555555",
		MatchID:     "11111111-1111-1111-1111-111111111111",
		WinnerID:    "22222222-2222-2222-2222-222222222222",
		Outcome:     OutcomeWin,
		TestsPassed: 2,
		TotalTests:  3,
	}
	raw, err := Encode(TypeMatchEnd, want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != TypeMatchEnd {
		t.Fatalf("type = %q, want %q", env.Type, TypeMatchEnd)
	}

	var got MatchEndData
	if err := env.DecodeData(&got); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestStableEventID(t *testing.T) {
	scope := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	recipient := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	want := uuid.MustParse("6e12cabe-af50-57d0-a645-3ca3780bea5d")

	first := StableEventID("submission-result", scope, recipient)
	second := StableEventID("submission-result", scope, recipient)
	if first != want || second != want {
		t.Fatalf("event IDs = (%s, %s), want %s", first, second, want)
	}
}

func TestEncodeDecodeError(t *testing.T) {
	want := ErrorData{Message: "unknown message type"}
	raw, err := Encode(TypeError, want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	var got ErrorData
	if err := env.DecodeData(&got); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEncodeDecodeErrorWithRequestID(t *testing.T) {
	want := ErrorData{
		Code:      "deadline_passed",
		Message:   "deadline passed",
		MatchID:   "11111111-1111-1111-1111-111111111111",
		RequestID: "22222222-2222-2222-2222-222222222222",
	}
	raw, err := Encode(TypeError, want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	env, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	var got ErrorData
	if err := env.DecodeData(&got); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestEncodeMissingType(t *testing.T) {
	if _, err := Encode("", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeMissingType(t *testing.T) {
	if _, err := Decode([]byte(`{"data":{}}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	if _, err := Decode([]byte(`{`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeRejectsUnknownEnvelopeFieldsAndTrailingValues(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"type":"join_queue","extra":true}`),
		[]byte(`{"type":"join_queue"} {"type":"join_queue"}`),
	} {
		if _, err := Decode(raw); err == nil {
			t.Fatalf("Decode(%s) returned nil error", raw)
		}
	}
}

func TestDecodeSubmitCodeDataStrictValidation(t *testing.T) {
	valid := `{"match_id":"11111111-1111-1111-1111-111111111111","request_id":"22222222-2222-2222-2222-222222222222","language":"python","code":"print(1)"}`
	tooLarge := strings.Repeat("x", MaxSubmissionCodeBytes+1)
	invalidUTF8 := append([]byte(`{"match_id":"11111111-1111-1111-1111-111111111111","request_id":"22222222-2222-2222-2222-222222222222","language":"python","code":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)

	tests := []struct {
		name string
		raw  []byte
	}{
		{"missing data", nil},
		{"unknown field", []byte(`{"match_id":"11111111-1111-1111-1111-111111111111","request_id":"22222222-2222-2222-2222-222222222222","language":"python","code":"print(1)","extra":true}`)},
		{"trailing data", []byte(valid + ` {}`)},
		{"nil match ID", []byte(`{"match_id":"00000000-0000-0000-0000-000000000000","request_id":"22222222-2222-2222-2222-222222222222","language":"python","code":"print(1)"}`)},
		{"invalid request ID", []byte(`{"match_id":"11111111-1111-1111-1111-111111111111","request_id":"not-a-uuid","language":"python","code":"print(1)"}`)},
		{"invalid language", []byte(`{"match_id":"11111111-1111-1111-1111-111111111111","request_id":"22222222-2222-2222-2222-222222222222","language":"Python","code":"print(1)"}`)},
		{"invalid UTF-8", invalidUTF8},
		{"NUL code", []byte(`{"match_id":"11111111-1111-1111-1111-111111111111","request_id":"22222222-2222-2222-2222-222222222222","language":"python","code":"print(\u0000)"}`)},
		{"whitespace code", []byte(`{"match_id":"11111111-1111-1111-1111-111111111111","request_id":"22222222-2222-2222-2222-222222222222","language":"python","code":" \t\n "}`)},
		{"too large", []byte(`{"match_id":"11111111-1111-1111-1111-111111111111","request_id":"22222222-2222-2222-2222-222222222222","language":"python","code":"` + tooLarge + `"}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeSubmitCodeData(tt.raw); err == nil {
				t.Fatal("DecodeSubmitCodeData returned nil error")
			}
		})
	}

	got, err := DecodeSubmitCodeData([]byte(valid))
	if err != nil {
		t.Fatalf("DecodeSubmitCodeData(valid): %v", err)
	}
	if got.Code != "print(1)" {
		t.Fatalf("code = %q, want original source", got.Code)
	}
}
