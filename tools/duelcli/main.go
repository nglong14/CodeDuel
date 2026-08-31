package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"

	"github.com/nglong14/CodeDuel/internal/gateway"
	"github.com/nglong14/CodeDuel/internal/proto"
)

const tokenTTL = 24 * time.Hour

type matchState struct {
	mu sync.RWMutex
	id uuid.UUID
}

func (s *matchState) get() uuid.UUID {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

func (s *matchState) set(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id = id
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "duelcli: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load()

	url := flag.String("url", "ws://localhost:8080/ws", "gateway WebSocket URL")
	user := flag.String("user", "", "user UUID to mint a JWT for")
	secret := flag.String("secret", os.Getenv("JWT_SECRET"), "JWT HMAC secret")
	token := flag.String("token", "", "pre-signed JWT (skips minting)")
	match := flag.String("match-id", "", "match UUID to use before receiving match_start")
	flag.Parse()

	jwt, err := resolveToken(*user, *secret, *token)
	if err != nil {
		return err
	}
	initialMatchID, err := parseMatchID(*match)
	if err != nil {
		return err
	}
	matches := &matchState{id: initialMatchID}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+jwt)

	conn, resp, err := websocket.DefaultDialer.Dial(*url, header)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("unauthorized")
			}
		}
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	fmt.Fprintf(os.Stderr, "connected to %s\n", *url)
	fmt.Fprintln(os.Stderr, "commands: join | submit <language> <code> | submit-file <language> <path>")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			rememberMatchStart(msg, matches)
			noteMatchEnd(msg)
			fmt.Println(string(msg))
		}
	}()
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for sc.Scan() {
			payload, err := parseIntent(sc.Text(), matches.get())
			if err != nil {
				fmt.Fprintf(os.Stderr, "duelcli: %v\n", err)
				continue
			}
			if payload == nil {
				continue
			}
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				errCh <- err
				return
			}
		}
		if err := sc.Err(); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second),
		)
		return nil
	case err := <-errCh:
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			return nil
		}
		if strings.Contains(err.Error(), "use of closed network connection") {
			return nil
		}
		return err
	}
}

func resolveToken(user, secret, token string) (string, error) {
	if token != "" {
		return token, nil
	}
	if user == "" {
		return "", fmt.Errorf("missing -user (or pass -token)")
	}
	userID, err := uuid.Parse(user)
	if err != nil {
		return "", fmt.Errorf("parse -user: %w", err)
	}
	if secret == "" {
		secret = "codeduel-dev-secret"
	}
	signed, err := gateway.MintToken(userID, secret, tokenTTL)
	if err != nil {
		return "", err
	}
	return signed, nil
}

func parseMatchID(raw string) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("parse -match-id: must be a non-nil UUID")
	}
	return id, nil
}

func rememberMatchStart(raw []byte, matches *matchState) {
	env, err := proto.Decode(raw)
	if err != nil || env.Type != proto.TypeMatchStart {
		return
	}
	var data proto.MatchStartData
	if err := env.DecodeData(&data); err != nil {
		return
	}
	matchID, err := uuid.Parse(data.MatchID)
	if err != nil || matchID == uuid.Nil {
		return
	}
	matches.set(matchID)
}

func noteMatchEnd(raw []byte) {
	env, err := proto.Decode(raw)
	if err != nil || env.Type != proto.TypeMatchEnd {
		return
	}
	var data proto.MatchEndData
	if err := env.DecodeData(&data); err != nil {
		return
	}
	if data.WinnerID != "" {
		fmt.Fprintf(os.Stderr, "match ended %s outcome=%s winner=%s tests=%d/%d\n",
			data.MatchID, data.Outcome, data.WinnerID, data.TestsPassed, data.TotalTests)
		return
	}
	fmt.Fprintf(os.Stderr, "match ended %s outcome=%s tests=%d/%d\n",
		data.MatchID, data.Outcome, data.TestsPassed, data.TotalTests)
}

func parseIntent(line string, matchID uuid.UUID) ([]byte, error) {
	if strings.TrimSpace(line) == "" {
		return nil, nil
	}
	line = strings.TrimLeft(line, " \t")
	cmd, rest, _ := strings.Cut(line, " ")
	switch strings.ToLower(cmd) {
	case "join":
		return proto.Encode(proto.TypeJoinQueue, nil)
	case "submit":
		language, code, ok := parseLanguageAndValue(rest)
		if !ok {
			return nil, fmt.Errorf("usage: submit <language> <code>")
		}
		return encodeSubmission(matchID, language, code)
	case "submit-file":
		language, path, ok := parseLanguageAndValue(rest)
		if !ok {
			return nil, fmt.Errorf("usage: submit-file <language> <path>")
		}
		code, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read submission file: %w", err)
		}
		return encodeSubmission(matchID, language, string(code))
	default:
		return nil, fmt.Errorf("unknown command %q (try: join, submit <language> <code>, submit-file <language> <path>)", cmd)
	}
}

func parseLanguageAndValue(raw string) (string, string, bool) {
	raw = strings.TrimLeft(raw, " \t")
	language, value, ok := strings.Cut(raw, " ")
	if !ok || language == "" {
		return "", "", false
	}
	value = strings.TrimLeft(value, " \t")
	if strings.TrimSpace(value) == "" {
		return "", "", false
	}
	return language, value, true
}

func encodeSubmission(matchID uuid.UUID, language, code string) ([]byte, error) {
	if matchID == uuid.Nil {
		return nil, fmt.Errorf("no remembered match ID; wait for match_start or pass -match-id")
	}
	data := proto.SubmitCodeData{
		MatchID:   matchID.String(),
		RequestID: uuid.NewString(),
		Language:  language,
		Code:      code,
	}
	if err := data.Validate(); err != nil {
		return nil, fmt.Errorf("invalid submission: %w", err)
	}
	return proto.Encode(proto.TypeSubmitCode, data)
}
