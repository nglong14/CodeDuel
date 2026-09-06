package gateway

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nglong14/CodeDuel/internal/infrastructure"
	"github.com/nglong14/CodeDuel/internal/redisx"
)

type recordingMatchmakingQueue struct {
	calls int
}

func (q *recordingMatchmakingQueue) Enqueue(context.Context, redisx.QueueMember) (redisx.EnqueueResult, error) {
	q.calls++
	return redisx.EnqueueResult{Added: true, Score: 1}, nil
}

func TestEnqueueForMatchRejectsActivePlayerIntegration(t *testing.T) {
	pool := gatewayIntegrationPostgres(t)
	ctx := context.Background()
	userID := testUserID()
	member := redisx.QueueMember{
		UserID:      userID,
		PresenceKey: redisx.PresenceKey(userID, uuid.New()),
		Route:       redisx.UserChannel(userID),
	}
	queue := &recordingMatchmakingQueue{}

	if err := enqueueForMatch(ctx, pool, queue, member); err != nil {
		t.Fatalf("enqueue eligible player: %v", err)
	}
	if queue.calls != 1 {
		t.Fatalf("queue calls = %d, want 1", queue.calls)
	}

	var matchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO matches (problem_id, status, deadline)
		SELECT id, 'active', clock_timestamp() + interval '1 hour'
		FROM problems
		ORDER BY created_at, id
		LIMIT 1
		RETURNING id
	`).Scan(&matchID); err != nil {
		t.Fatalf("insert active match: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO match_players (match_id, user_id, slot)
		VALUES ($1, $2, 1)
	`, matchID, userID); err != nil {
		t.Fatalf("insert active membership: %v", err)
	}

	err := enqueueForMatch(ctx, pool, queue, member)
	var activeErr *alreadyInActiveMatchError
	if !errors.As(err, &activeErr) || activeErr.matchID != matchID {
		t.Fatalf("enqueue active player error = %v, want match %s", err, matchID)
	}
	if queue.calls != 1 {
		t.Fatalf("queue calls = %d, want no call for active player", queue.calls)
	}
}

func gatewayIntegrationPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("CODEDUEL_INTEGRATION") != "1" {
		t.Skip("set CODEDUEL_INTEGRATION=1 to run integration tests")
	}
	baseDSN := os.Getenv("POSTGRES_TEST_DSN")
	if baseDSN == "" {
		baseDSN = "postgres://codeduel:codeduel@localhost:5433/postgres?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, baseDSN)
	if err != nil {
		t.Fatalf("open admin PostgreSQL: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Fatalf("connect to integration PostgreSQL: %v", err)
	}
	database := "codeduel_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+database); err != nil {
		admin.Close()
		t.Fatalf("create integration database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+database+" WITH (FORCE)")
		admin.Close()
	})

	testURL, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse POSTGRES_TEST_DSN: %v", err)
	}
	testURL.Path = "/" + database
	testDSN := testURL.String()
	if err := infrastructure.MigrateUp(ctx, testDSN); err != nil {
		t.Fatalf("migrate integration database: %v", err)
	}
	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("open integration database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
