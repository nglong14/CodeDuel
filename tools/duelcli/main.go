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
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"

	"github.com/nglong14/CodeDuel/internal/gateway"
	"github.com/nglong14/CodeDuel/internal/proto"
)

const tokenTTL = 24 * time.Hour

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
	flag.Parse()

	jwt, err := resolveToken(*user, *secret, *token)
	if err != nil {
		return err
	}

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
	fmt.Fprintln(os.Stderr, "commands: join | submit <language> <code>")

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
			fmt.Println(string(msg))
		}
	}()
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for sc.Scan() {
			payload, err := parseIntent(sc.Text())
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

func parseIntent(line string) ([]byte, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}
	cmd, rest, _ := strings.Cut(line, " ")
	switch strings.ToLower(cmd) {
	case "join":
		return proto.Encode(proto.TypeJoinQueue, nil)
	case "submit":
		lang, code, ok := strings.Cut(strings.TrimSpace(rest), " ")
		if !ok || lang == "" || strings.TrimSpace(code) == "" {
			return nil, fmt.Errorf("usage: submit <language> <code>")
		}
		return proto.Encode(proto.TypeSubmitCode, proto.SubmitCodeData{
			Language: lang,
			Code:     strings.TrimSpace(code),
		})
	default:
		return nil, fmt.Errorf("unknown command %q (try: join, submit <language> <code>)", cmd)
	}
}
