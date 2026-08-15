package main

import (
	"testing"

	"github.com/google/uuid"

	"github.com/nglong14/CodeDuel/internal/proto"
)

func TestParseIntentJoin(t *testing.T) {
	raw, err := parseIntent("join")
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
	raw, err := parseIntent(`submit python print("hello world")`)
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
}

func TestParseIntentRejects(t *testing.T) {
	if _, err := parseIntent("nope"); err == nil {
		t.Fatal("expected error for unknown command")
	}
	if _, err := parseIntent("submit python"); err == nil {
		t.Fatal("expected error for incomplete submit")
	}
	raw, err := parseIntent("   ")
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
