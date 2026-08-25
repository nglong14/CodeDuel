package infrastructure

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSubmissionLifecycleMigrationBackfillsAndRollsBack(t *testing.T) {
	if os.Getenv("CODEDUEL_INTEGRATION") != "1" {
		t.Skip("set CODEDUEL_INTEGRATION=1 to run integration tests")
	}
	ctx := context.Background()
	baseDSN := os.Getenv("POSTGRES_TEST_DSN")
	if baseDSN == "" {
		baseDSN = "postgres://codeduel:codeduel@localhost:5433/postgres?sslmode=disable"
	}
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
	legacyMigrator, err := newMigrator(testDSN)
	if err != nil {
		t.Fatalf("create legacy migrator: %v", err)
	}
	if err := legacyMigrator.Migrate(3); err != nil {
		_, _ = legacyMigrator.Close()
		t.Fatalf("migrate through Phase 3: %v", err)
	}
	_, _ = legacyMigrator.Close()

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	var problemID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM problems ORDER BY created_at, id LIMIT 1`).Scan(&problemID); err != nil {
		pool.Close()
		t.Fatalf("select seeded problem: %v", err)
	}
	playerID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	nonPlayerID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	matchID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO matches (id, problem_id, winner_id, deadline)
		VALUES ($1, $2, $3, now() + interval '1 hour')
	`, matchID, problemID, nonPlayerID); err != nil {
		pool.Close()
		t.Fatalf("insert legacy match: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO match_players (match_id, user_id, slot) VALUES ($1, $2, 1)`, matchID, playerID); err != nil {
		pool.Close()
		t.Fatalf("insert legacy player: %v", err)
	}
	pendingID := uuid.New()
	completedID := uuid.New()
	createdAt := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (id, match_id, player_id, language, code, result, created_at)
		VALUES ($1, $2, $3, 'python', 'print(1)', 'pending', $4),
		       ($5, $2, $3, 'python', 'print(2)', 'pass', $4)
	`, pendingID, matchID, playerID, createdAt, completedID); err != nil {
		pool.Close()
		t.Fatalf("insert legacy submissions: %v", err)
	}
	pool.Close()

	if err := MigrateUp(ctx, testDSN); err != nil {
		t.Fatalf("apply lifecycle migration: %v", err)
	}
	pool, err = pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer pool.Close()

	assertMigratedSubmission(t, pool, pendingID, "pending", nil, nil, pendingID)
	pass := "pass"
	assertMigratedSubmission(t, pool, completedID, "completed", &pass, &createdAt, completedID)
	var winnerID *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT winner_id FROM matches WHERE id = $1`, matchID).Scan(&winnerID); err != nil {
		t.Fatalf("query migrated winner: %v", err)
	}
	if winnerID != nil {
		t.Fatalf("winner ID = %s, want NULL for legacy non-player", *winnerID)
	}

	pool.Close()
	rollbackMigrator, err := newMigrator(testDSN)
	if err != nil {
		t.Fatalf("create rollback migrator: %v", err)
	}
	if err := rollbackMigrator.Steps(-1); err != nil && err != migrate.ErrNoChange {
		_, _ = rollbackMigrator.Close()
		t.Fatalf("roll back lifecycle migration: %v", err)
	}
	_, _ = rollbackMigrator.Close()
	pool, err = pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("open rolled back database: %v", err)
	}
	defer pool.Close()
	var result string
	if err := pool.QueryRow(ctx, `SELECT result FROM submissions WHERE id = $1`, pendingID).Scan(&result); err != nil {
		t.Fatalf("query rolled back submission: %v", err)
	}
	if result != "pending" {
		t.Fatalf("rolled back result = %q, want pending", result)
	}
}

func assertMigratedSubmission(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, wantStatus string, wantResult *string, wantFinishedAt *time.Time, wantRequestID uuid.UUID) {
	t.Helper()
	var status string
	var result *string
	var finishedAt *time.Time
	var requestID uuid.UUID
	if err := pool.QueryRow(context.Background(), `
		SELECT status, result, finished_at, request_id
		FROM submissions
		WHERE id = $1
	`, id).Scan(&status, &result, &finishedAt, &requestID); err != nil {
		t.Fatalf("query migrated submission: %v", err)
	}
	if status != wantStatus || requestID != wantRequestID {
		t.Fatalf("submission state = status %q, request ID %s", status, requestID)
	}
	if (result == nil) != (wantResult == nil) || result != nil && *result != *wantResult {
		t.Fatalf("result = %v, want %v", result, wantResult)
	}
	if (finishedAt == nil) != (wantFinishedAt == nil) || finishedAt != nil && !finishedAt.Equal(*wantFinishedAt) {
		t.Fatalf("finished_at = %v, want %v", finishedAt, wantFinishedAt)
	}
}
