package infrastructure

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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
	if err := rollbackMigrator.Migrate(3); err != nil && err != migrate.ErrNoChange {
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

func TestAuthMigrationNormalizesBackfillsAndRollsBack(t *testing.T) {
	if os.Getenv("CODEDUEL_INTEGRATION") != "1" {
		t.Skip("set CODEDUEL_INTEGRATION=1 to run integration tests")
	}
	ctx := context.Background()
	admin, database := newAdminPoolAndDatabase(t)
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+database+" WITH (FORCE)")
		admin.Close()
	})
	testDSN := databaseDSN(t, admin, database)

	legacyMigrator, err := newMigrator(testDSN)
	if err != nil {
		t.Fatalf("create legacy migrator: %v", err)
	}
	if err := legacyMigrator.Migrate(5); err != nil {
		_, _ = legacyMigrator.Close()
		t.Fatalf("migrate through version 5: %v", err)
	}
	_, _ = legacyMigrator.Close()

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer pool.Close()

	legacyID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email)
		VALUES ($1, '  Padded.User@Example.COM ')
	`, legacyID); err != nil {
		t.Fatalf("insert legacy padded user: %v", err)
	}

	authMigrator, err := newMigrator(testDSN)
	if err != nil {
		t.Fatalf("create auth migrator: %v", err)
	}
	if err := authMigrator.Migrate(6); err != nil {
		_, _ = authMigrator.Close()
		t.Fatalf("migrate through version 6: %v", err)
	}
	_, _ = authMigrator.Close()

	var email, displayName string
	var passwordHash *string
	if err := pool.QueryRow(ctx, `SELECT email, display_name, password_hash FROM users WHERE id = $1`, legacyID).Scan(&email, &displayName, &passwordHash); err != nil {
		t.Fatalf("query normalized user: %v", err)
	}
	if email != "padded.user@example.com" || displayName != "padded.user" || passwordHash != nil {
		t.Fatalf("legacy user after migration = email %q, display %q, hash %v", email, displayName, passwordHash)
	}
	if err := pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id = '11111111-1111-1111-1111-111111111111'`).Scan(&displayName); err != nil {
		t.Fatalf("query seeded display name: %v", err)
	}
	if displayName != "alice" {
		t.Fatalf("seeded alice display name = %q, want alice", displayName)
	}

	newID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, display_name)
		VALUES ($1, $2::text || '@fresh.test', $2::text)
	`, newID, newID.String()); err != nil {
		t.Fatalf("insert user without password hash: %v", err)
	}

	assertConstraintViolation := func(name, sql string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, sql, args...)
		if err == nil {
			t.Fatalf("%s: expected constraint violation", name)
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) {
			t.Fatalf("%s: expected pg error, got %v", name, err)
		}
		if pgErr.Code != "23502" && pgErr.Code != "23505" && pgErr.Code != "23514" {
			t.Fatalf("%s: unexpected pg code %s (%v)", name, pgErr.Code, err)
		}
	}
	assertConstraintViolation("missing email", `INSERT INTO users (id, display_name) VALUES ($1, 'noname')`, uuid.New())
	assertConstraintViolation("missing display name", `INSERT INTO users (id, email) VALUES ($1, 'missingname@example.com')`, uuid.New())
	assertConstraintViolation("un-normalized email", `INSERT INTO users (id, email, display_name) VALUES ($1, 'Alice@Example.com', 'alice')`, uuid.New())
	assertConstraintViolation("blank display name", `INSERT INTO users (id, email, display_name) VALUES ($1, 'blank@example.com', '  ')`, uuid.New())
	assertConstraintViolation("duplicate normalized email", `INSERT INTO users (id, email, display_name) VALUES ($1, 'padded.user@example.com', 'padded.user')`, uuid.New())

	rollbackMigrator, err := newMigrator(testDSN)
	if err != nil {
		t.Fatalf("create rollback migrator: %v", err)
	}
	if err := rollbackMigrator.Migrate(5); err != nil {
		_, _ = rollbackMigrator.Close()
		t.Fatalf("roll back auth migration: %v", err)
	}
	_, _ = rollbackMigrator.Close()

	var dropped bool
	err = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'display_name')`).Scan(&dropped)
	if err != nil {
		t.Fatalf("query display_name column presence: %v", err)
	}
	if dropped {
		t.Fatal("display_name column still exists after rollback")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id) VALUES ($1)`, uuid.New()); err != nil {
		t.Fatalf("insert UUID-only user after rollback: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, legacyID).Scan(&email); err != nil {
		t.Fatalf("query rolled back email: %v", err)
	}
	if email != "padded.user@example.com" {
		t.Fatalf("rolled back email = %q, want normalized spelling preserved", email)
	}
}

func newAdminPoolAndDatabase(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
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
	return admin, database
}

func databaseDSN(t *testing.T, admin *pgxpool.Pool, database string) string {
	t.Helper()
	baseDSN := os.Getenv("POSTGRES_TEST_DSN")
	if baseDSN == "" {
		baseDSN = "postgres://codeduel:codeduel@localhost:5433/postgres?sslmode=disable"
	}
	testURL, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse POSTGRES_TEST_DSN: %v", err)
	}
	testURL.Path = "/" + database
	return testURL.String()
}
