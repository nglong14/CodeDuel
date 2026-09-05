package submission

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nglong14/CodeDuel/internal/infrastructure"
)

var (
	integrationPlayerOne = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	integrationPlayerTwo = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

func TestAcceptIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	ctx := context.Background()
	matchID := createMatch(t, pool, "active", time.Hour, integrationPlayerOne)
	service := NewService(pool)
	request := Request{
		PlayerID:  integrationPlayerOne,
		MatchID:   matchID,
		RequestID: uuid.New(),
		Language:  "python",
		Code:      "print(1)",
	}

	id, err := service.Accept(ctx, request)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("Accept returned nil submission ID")
	}

	var status string
	var result *string
	var attempts int
	if err := pool.QueryRow(ctx, `
		SELECT status, result, attempts
		FROM submissions
		WHERE id = $1
	`, id).Scan(&status, &result, &attempts); err != nil {
		t.Fatalf("query submission: %v", err)
	}
	if status != "pending" || result != nil || attempts != 0 {
		t.Fatalf("submission lifecycle = status %q, result %v, attempts %d", status, result, attempts)
	}

	if _, err := pool.Exec(ctx, `UPDATE submissions SET status = 'completed' WHERE id = $1`, id); err == nil {
		t.Fatal("completed submission without terminal result was accepted")
	}
}

func TestAcceptIdempotencyIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	ctx := context.Background()
	matchID := createMatch(t, pool, "active", time.Hour, integrationPlayerOne)
	service := NewService(pool)
	request := Request{
		PlayerID:  integrationPlayerOne,
		MatchID:   matchID,
		RequestID: uuid.New(),
		Language:  "python",
		Code:      "print(1)",
	}

	first, err := service.Accept(ctx, request)
	if err != nil {
		t.Fatalf("first Accept: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE matches SET status = 'finished' WHERE id = $1`, matchID); err != nil {
		t.Fatalf("finish match: %v", err)
	}
	second, err := service.Accept(ctx, request)
	if err != nil {
		t.Fatalf("idempotent Accept: %v", err)
	}
	if second != first {
		t.Fatalf("idempotent ID = %s, want %s", second, first)
	}

	request.Code = "print(2)"
	if _, err := service.Accept(ctx, request); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting Accept error = %v, want %v", err, ErrIdempotencyConflict)
	}
}

func TestAcceptRejectsInvalidMatchStateIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	ctx := context.Background()
	service := NewService(pool)

	tests := []struct {
		name    string
		matchID func() uuid.UUID
		player  uuid.UUID
		want    error
	}{
		{
			name:    "unknown match",
			matchID: uuid.New,
			player:  integrationPlayerOne,
			want:    ErrMatchNotFound,
		},
		{
			name: "not a player",
			matchID: func() uuid.UUID {
				return createMatch(t, pool, "active", time.Hour, integrationPlayerOne)
			},
			player: integrationPlayerTwo,
			want:   ErrNotMatchPlayer,
		},
		{
			name: "finished match",
			matchID: func() uuid.UUID {
				return createMatch(t, pool, "finished", time.Hour, integrationPlayerOne)
			},
			player: integrationPlayerOne,
			want:   ErrMatchNotActive,
		},
		{
			name: "expired match",
			matchID: func() uuid.UUID {
				return createMatch(t, pool, "active", -time.Hour, integrationPlayerTwo)
			},
			player: integrationPlayerTwo,
			want:   ErrDeadlinePassed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Accept(ctx, Request{
				PlayerID:  tt.player,
				MatchID:   tt.matchID(),
				RequestID: uuid.New(),
				Language:  "python",
				Code:      "print(1)",
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Accept error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAcceptConcurrentRetryIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	ctx := context.Background()
	matchID := createMatch(t, pool, "active", time.Hour, integrationPlayerOne)
	service := NewService(pool)
	request := Request{
		PlayerID:  integrationPlayerOne,
		MatchID:   matchID,
		RequestID: uuid.New(),
		Language:  "python",
		Code:      "print(1)",
	}

	ids := make([]uuid.UUID, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ids[i], errs[i] = service.Accept(ctx, request)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Accept %d: %v", i, err)
		}
	}
	if ids[0] != ids[1] {
		t.Fatalf("concurrent IDs = %s and %s", ids[0], ids[1])
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM submissions WHERE player_id = $1 AND request_id = $2`, request.PlayerID, request.RequestID).Scan(&count); err != nil {
		t.Fatalf("count submissions: %v", err)
	}
	if count != 1 {
		t.Fatalf("submission count = %d, want 1", count)
	}
}

func TestMembershipConstraintsIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	ctx := context.Background()
	matchID := createMatch(t, pool, "active", time.Hour, integrationPlayerOne)

	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (match_id, player_id, request_id, language, code)
		VALUES ($1, $2, $3, 'python', 'print(1)')
	`, matchID, integrationPlayerTwo, uuid.New()); err == nil {
		t.Fatal("submission for non-player was accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE matches SET winner_id = $1 WHERE id = $2`, integrationPlayerTwo, matchID); err == nil {
		t.Fatal("winner who is not a player was accepted")
	}
	if _, err := pool.Exec(ctx, `UPDATE matches SET winner_id = $1 WHERE id = $2`, integrationPlayerOne, matchID); err != nil {
		t.Fatalf("set player winner: %v", err)
	}
}

func createMatch(t *testing.T, pool *pgxpool.Pool, status string, deadlineOffset time.Duration, players ...uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var problemID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM problems ORDER BY created_at, id LIMIT 1`).Scan(&problemID); err != nil {
		t.Fatalf("select problem: %v", err)
	}
	var matchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO matches (problem_id, status, deadline)
		VALUES ($1, $2, now() + ($3 * interval '1 microsecond'))
		RETURNING id
	`, problemID, status, deadlineOffset.Microseconds()).Scan(&matchID); err != nil {
		t.Fatalf("insert match: %v", err)
	}
	for i, playerID := range players {
		if _, err := pool.Exec(ctx, `INSERT INTO match_players (match_id, user_id, slot) VALUES ($1, $2, $3)`, matchID, playerID, i+1); err != nil {
			t.Fatalf("insert match player: %v", err)
		}
	}
	return matchID
}

func integrationPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("CODEDUEL_INTEGRATION") != "1" {
		t.Skip("set CODEDUEL_INTEGRATION=1 to run integration tests")
	}
	baseDSN := os.Getenv("POSTGRES_TEST_DSN")
	if baseDSN == "" {
		baseDSN = "postgres://codeduel:codeduel@localhost:5433/postgres?sslmode=disable"
	}
	ctx := context.Background()
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
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+database+" WITH (FORCE)")
		admin.Close()
	})
	return pool
}
