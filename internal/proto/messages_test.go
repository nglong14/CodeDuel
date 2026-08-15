package proto

import (
	"testing"
	"time"
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

func TestEncodeDecodeSubmitCode(t *testing.T) {
	want := SubmitCodeData{Language: "python", Code: "print(1)"}
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

	var got SubmitCodeData
	if err := env.DecodeData(&got); err != nil {
		t.Fatalf("DecodeData: %v", err)
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
	want := JudgingData{SubmissionID: "33333333-3333-3333-3333-333333333333"}
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
	want := ResultData{
		MatchID:     "11111111-1111-1111-1111-111111111111",
		WinnerID:    "44444444-4444-4444-4444-444444444444",
		TestsPassed: 3,
		Outcome:     "win",
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
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
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
