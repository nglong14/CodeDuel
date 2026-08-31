package main

import (
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/nglong14/CodeDuel/internal/proto"
)

func TestParseIntentJoin(t *testing.T) {
	raw, err := parseIntent("join", uuid.Nil)
	if err != nil {
		t.Fatalf("parseIntent: %v", err)
	}
	env, err := proto.Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != proto.TypeJoinQueue {
		t.Fatalf("type = %q, want %q", env.Type, proto.TypeJoinQueue)
	}
}

func TestParseIntentSubmit(t *testing.T) {
	matchID := uuid.New()
	raw, err := parseIntent(`submit python print("hello world")`, matchID)
	if err != nil {
		t.Fatalf("parseIntent: %v", err)
	}
	env, err := proto.Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	var data proto.SubmitCodeData
	if err := env.DecodeData(&data); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if data.Language != "python" {
		t.Fatalf("language = %q, want python", data.Language)
	}
	if data.Code != `print("hello world")` {
		t.Fatalf("code = %q", data.Code)
	}
	if data.MatchID != matchID.String() {
		t.Fatalf("match_id = %q, want %q", data.MatchID, matchID)
	}
	if requestID, err := uuid.Parse(data.RequestID); err != nil || requestID == uuid.Nil {
		t.Fatalf("request_id = %q", data.RequestID)
	}
}

func TestParseIntentSubmitFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "submission-*.py")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	code := "print('hello')\n"
	if _, err := file.WriteString(code); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := parseIntent("submit-file python "+file.Name(), uuid.New())
	if err != nil {
		t.Fatalf("parseIntent: %v", err)
	}
	env, err := proto.Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	var data proto.SubmitCodeData
	if err := env.DecodeData(&data); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if data.Code != code {
		t.Fatalf("code = %q, want %q", data.Code, code)
	}
}

func TestRememberMatchStart(t *testing.T) {
	state := &matchState{}
	matchID := uuid.New()
	raw, err := proto.Encode(proto.TypeMatchStart, proto.MatchStartData{MatchID: matchID.String()})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	rememberMatchStart(raw, state)
	if state.get() != matchID {
		t.Fatalf("remembered match ID = %s, want %s", state.get(), matchID)
	}
}

func TestNoteMatchEnd(t *testing.T) {
	raw, err := proto.Encode(proto.TypeMatchEnd, proto.MatchEndData{
		EventID:     uuid.NewString(),
		MatchID:     uuid.NewString(),
		Outcome:     proto.OutcomeDraw,
		TestsPassed: 1,
		TotalTests:  3,
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	noteMatchEnd(raw)
	noteMatchEnd([]byte(`{"type":"result","data":{}}`))
}

func TestParseIntentRejects(t *testing.T) {
	if _, err := parseIntent("nope", uuid.Nil); err == nil {
		t.Fatal("expected error for unknown command")
	}
	if _, err := parseIntent("submit python", uuid.New()); err == nil {
		t.Fatal("expected error for incomplete submit")
	}
	if _, err := parseIntent("submit python print(1)", uuid.Nil); err == nil {
		t.Fatal("expected error without remembered match")
	}
	raw, err := parseIntent("   ", uuid.Nil)
	if err != nil {
		t.Fatalf("blank line: %v", err)
	}
	if raw != nil {
		t.Fatal("blank line should be a no-op")
	}
}

func TestResolveTokenPrefersRawToken(t *testing.T) {
	got, err := resolveToken("", "", "already-signed")
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if got != "already-signed" {
		t.Fatalf("got %q, want already-signed", got)
	}
}

func TestResolveTokenMints(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got, err := resolveToken(userID.String(), "test-secret", "")
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if got == "" {
		t.Fatal("expected minted token")
	}
}

func TestResolveTokenRequiresUser(t *testing.T) {
	if _, err := resolveToken("", "secret", ""); err == nil {
		t.Fatal("expected error")
	}
}
