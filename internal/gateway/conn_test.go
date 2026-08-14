package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/nglong14/CodeDuel/internal/proto"
)

func TestHandleInboundJoinQueueEcho(t *testing.T) {
	raw, err := proto.Encode(proto.TypeJoinQueue, nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	resp, err := handleInbound(raw)
	if err != nil {
		t.Fatalf("handleInbound: %v", err)
	}

	env, err := proto.Decode(resp)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != proto.TypeJoinQueue {
		t.Fatalf("type = %q, want %q", env.Type, proto.TypeJoinQueue)
	}
}

func TestHandleInboundSubmitCodeJudging(t *testing.T) {
	raw, err := proto.Encode(proto.TypeSubmitCode, proto.SubmitCodeData{
		Language: "python",
		Code:     "print(1)",
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	resp, err := handleInbound(raw)
	if err != nil {
		t.Fatalf("handleInbound: %v", err)
	}

	env, err := proto.Decode(resp)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != proto.TypeJudging {
		t.Fatalf("type = %q, want %q", env.Type, proto.TypeJudging)
	}

	var data proto.JudgingData
	if err := env.DecodeData(&data); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if _, err := uuid.Parse(data.SubmissionID); err != nil {
		t.Fatalf("submission_id %q: %v", data.SubmissionID, err)
	}
}

func TestConnCleanupRemovesAndOnClose(t *testing.T) {
	r := NewRegistry()
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	c := newConn(userID, nil, r)
	called := false
	c.onClose = func() { called = true }

	r.Add(c)
	c.cleanup()

	if !called {
		t.Fatal("onClose not called")
	}
	if got := r.Get(userID); got != nil {
		t.Fatalf("Get after cleanup = %p, want nil", got)
	}
	select {
	case <-c.closed:
	default:
		t.Fatal("expected conn to be closed")
	}
}

func TestHandleInboundRejects(t *testing.T) {
	emptySubmit, err := proto.Encode(proto.TypeSubmitCode, proto.SubmitCodeData{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	unknown, err := proto.Encode("not_a_type", nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	tests := []struct {
		name    string
		raw     []byte
		wantMsg string
	}{
		{"invalid json", []byte(`{`), "invalid message"},
		{"missing type", []byte(`{"data":{}}`), "invalid message"},
		{"unknown type", unknown, "unknown message type"},
		{"empty submit", emptySubmit, "invalid submit_code"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := handleInbound(tt.raw)
			if err != nil {
				t.Fatalf("handleInbound: %v", err)
			}
			env, err := proto.Decode(resp)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if env.Type != proto.TypeError {
				t.Fatalf("type = %q, want %q", env.Type, proto.TypeError)
			}
			var data proto.ErrorData
			if err := env.DecodeData(&data); err != nil {
				t.Fatalf("DecodeData: %v", err)
			}
			if data.Message != tt.wantMsg {
				t.Fatalf("message = %q, want %q", data.Message, tt.wantMsg)
			}
		})
	}
}

func TestConnPumpsEchoAndReplace(t *testing.T) {
	registry := NewRegistry()
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := newConn(userID, ws, registry)
		registry.Add(c)
		c.serve()
	}))
	defer s.Close()

	wsURL := "ws" + strings.TrimPrefix(s.URL, "http")
	first, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial first: %v", err)
	}
	defer func() { _ = first.Close() }()

	join, err := proto.Encode(proto.TypeJoinQueue, nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := first.WriteMessage(websocket.TextMessage, join); err != nil {
		t.Fatalf("write join: %v", err)
	}
	assertType(t, first, proto.TypeJoinQueue)

	submit, err := proto.Encode(proto.TypeSubmitCode, proto.SubmitCodeData{
		Language: "python",
		Code:     "print(1)",
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := first.WriteMessage(websocket.TextMessage, submit); err != nil {
		t.Fatalf("write submit: %v", err)
	}
	assertType(t, first, proto.TypeJudging)

	second, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial second: %v", err)
	}
	defer func() { _ = second.Close() }()

	_ = first.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := first.ReadMessage(); err == nil {
		t.Fatal("expected first connection to close after replace")
	}

	if err := second.WriteMessage(websocket.TextMessage, join); err != nil {
		t.Fatalf("write join on replacement: %v", err)
	}
	assertType(t, second, proto.TypeJoinQueue)
}

func assertType(t *testing.T, ws *websocket.Conn, want string) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	env, err := proto.Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != want {
		t.Fatalf("type = %q, want %q", env.Type, want)
	}
}
