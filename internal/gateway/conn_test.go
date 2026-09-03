package gateway

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/nglong14/CodeDuel/internal/proto"
	"github.com/nglong14/CodeDuel/internal/redisx"
	"github.com/nglong14/CodeDuel/internal/submission"
)

func TestHandleInboundJoinQueueEnqueuesWithoutResponse(t *testing.T) {
	raw, err := proto.Encode(proto.TypeJoinQueue, nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	c := newConn(userID, nil, NewRegistry())
	var got redisx.QueueMember
	c.enqueue = func(_ context.Context, member redisx.QueueMember) error {
		got = member
		return nil
	}
	resp, err := c.handleInbound(raw)
	if err != nil {
		t.Fatalf("conn.handleInbound: %v", err)
	}
	if resp != nil {
		t.Fatalf("response = %s, want nil", resp)
	}
	if got.UserID != userID || got.PresenceKey != c.presenceKey || got.Route != redisx.UserChannel(userID) {
		t.Fatalf("enqueued member = %#v", got)
	}
}

func TestHandleInboundSubmitCodeJudging(t *testing.T) {
	userID := uuid.New()
	matchID := uuid.New()
	requestID := uuid.New()
	submissionID := uuid.New()
	raw, err := proto.Encode(proto.TypeSubmitCode, proto.SubmitCodeData{
		MatchID:   matchID.String(),
		RequestID: requestID.String(),
		Language:  "python",
		Code:      "print(1)",
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	c := newConn(userID, nil, NewRegistry())
	var got submission.Request
	c.acceptSubmission = func(_ context.Context, request submission.Request) (uuid.UUID, error) {
		got = request
		return submissionID, nil
	}
	resp, err := c.handleInbound(raw)
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
	if data.SubmissionID != submissionID.String() {
		t.Fatalf("submission_id = %q, want %q", data.SubmissionID, submissionID)
	}
	if got.PlayerID != userID || got.MatchID != matchID || got.RequestID != requestID || got.Language != "python" || got.Code != "print(1)" {
		t.Fatalf("submission request = %#v", got)
	}
}

func TestHandleInboundSubmitCodeMapsServiceErrors(t *testing.T) {
	raw, err := proto.Encode(proto.TypeSubmitCode, proto.SubmitCodeData{
		MatchID:   uuid.NewString(),
		RequestID: uuid.NewString(),
		Language:  "python",
		Code:      "print(1)",
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"invalid request", submission.ErrInvalidRequest, "invalid request"},
		{"not a player", submission.ErrNotMatchPlayer, "not a match player"},
		{"deadline", submission.ErrDeadlinePassed, "deadline passed"},
		{"not found", submission.ErrMatchNotFound, "match not active"},
		{"inactive", submission.ErrMatchNotActive, "match not active"},
		{"conflict", submission.ErrIdempotencyConflict, "idempotency conflict"},
		{"database", errors.New("database unavailable"), "unable to accept submission"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConn(uuid.New(), nil, NewRegistry())
			c.acceptSubmission = func(context.Context, submission.Request) (uuid.UUID, error) {
				return uuid.Nil, tt.err
			}
			resp, handleErr := c.handleInbound(raw)
			if handleErr != nil {
				t.Fatalf("handleInbound: %v", handleErr)
			}
			assertErrorMessage(t, resp, tt.want)
		})
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
	r.Wait()

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

func TestConnectionPresenceIsSpecificToConnection(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	first := newConn(userID, nil, NewRegistry())
	second := newConn(userID, nil, NewRegistry())
	if first.connectionID == second.connectionID {
		t.Fatal("connection IDs are equal")
	}
	if first.presenceKey == second.presenceKey {
		t.Fatal("presence keys are equal")
	}
	if first.route != second.route || first.route != redisx.UserChannel(userID) {
		t.Fatalf("routes = %q and %q", first.route, second.route)
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

	invalidJoin := []byte(`{"type":"join_queue","data":{"user_id":"attacker"}}`)
	c := newConn(uuid.New(), nil, NewRegistry())
	c.enqueue = func(context.Context, redisx.QueueMember) error { return nil }

	tests := []struct {
		name    string
		raw     []byte
		wantMsg string
	}{
		{"invalid json", []byte(`{`), "invalid message"},
		{"missing type", []byte(`{"data":{}}`), "invalid message"},
		{"unknown type", unknown, "unknown message type"},
		{"empty submit", emptySubmit, "invalid submit_code"},
		{"join with fields", invalidJoin, "invalid join_queue"},
		{"submit unknown field", []byte(`{"type":"submit_code","data":{"match_id":"11111111-1111-1111-1111-111111111111","request_id":"22222222-2222-2222-2222-222222222222","language":"python","code":"print(1)","extra":true}}`), "invalid submit_code"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := c.handleInbound(tt.raw)
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

func TestHandleInboundEnqueueFailureIsRetryable(t *testing.T) {
	c := newConn(uuid.New(), nil, NewRegistry())
	c.enqueue = func(context.Context, redisx.QueueMember) error { return errors.New("redis unavailable") }
	join, err := proto.Encode(proto.TypeJoinQueue, nil)
	if err != nil {
		t.Fatalf("Encode join: %v", err)
	}
	resp, err := c.handleInbound(join)
	if err != nil {
		t.Fatalf("handleInbound join: %v", err)
	}
	assertErrorMessage(t, resp, "unable to join queue")

	c.enqueue = func(context.Context, redisx.QueueMember) error { return nil }
	resp, err = c.handleInbound(join)
	if err != nil {
		t.Fatalf("handleInbound retry: %v", err)
	}
	if resp != nil {
		t.Fatalf("retry response = %s, want nil", resp)
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
		c.enqueue = func(context.Context, redisx.QueueMember) error { return nil }
		c.acceptSubmission = func(context.Context, submission.Request) (uuid.UUID, error) { return uuid.New(), nil }
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
	submit, err := proto.Encode(proto.TypeSubmitCode, proto.SubmitCodeData{
		MatchID:   uuid.NewString(),
		RequestID: uuid.NewString(),
		Language:  "python",
		Code:      "print(1)",
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
	if err := second.WriteMessage(websocket.TextMessage, submit); err != nil {
		t.Fatalf("write submit on replacement: %v", err)
	}
	assertType(t, second, proto.TypeJudging)
}

func TestConnClosesWhenTokenExpires(t *testing.T) {
	registry := NewRegistry()
	userID := testUserID()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := newConn(userID, ws, registry)
		c.tokenExpiresAt = time.Now().Add(100 * time.Millisecond)
		registry.Add(c)
		c.serve()
	}))
	defer s.Close()

	wsURL := "ws" + strings.TrimPrefix(s.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	messageType, payload, err := client.ReadMessage()
	if err != nil {
		var closeErr *websocket.CloseError
		if errors.As(err, &closeErr) && closeErr.Code == websocket.ClosePolicyViolation {
			return
		}
		t.Fatalf("read close frame: %v", err)
	}
	if messageType != websocket.CloseMessage {
		t.Fatalf("message type = %v, want close", messageType)
	}
	if len(payload) < 2 {
		t.Fatal("close payload is too short")
	}
	if code := int(binary.BigEndian.Uint16(payload)); code != websocket.ClosePolicyViolation {
		t.Fatalf("close code = %d, want %d", code, websocket.ClosePolicyViolation)
	}
}

func assertErrorMessage(t *testing.T, raw []byte, want string) {
	t.Helper()
	env, err := proto.Decode(raw)
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
	if data.Message != want {
		t.Fatalf("message = %q, want %q", data.Message, want)
	}
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
