package match

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nglong14/CodeDuel/internal/infrastructure"
)

func TestCreateMatchIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	ctx := context.Background()
	players := testPlayers()

	before := time.Now()
	created, err := createMatch(ctx, pool, 2*time.Minute, players)
	if err != nil {
		t.Fatalf("createMatch: %v", err)
	}
	if created.MatchID == uuid.Nil || created.ProblemID == uuid.Nil {
		t.Fatalf("created match = %#v", created)
	}
	if created.Deadline.Before(before.Add(2*time.Minute-time.Second)) || created.Deadline.After(time.Now().Add(2*time.Minute+time.Second)) {
		t.Fatalf("deadline = %v", created.Deadline)
	}

	rows, err := pool.Query(ctx, `SELECT user_id, slot FROM match_players WHERE match_id = $1 ORDER BY slot`, created.MatchID)
	if err != nil {
		t.Fatalf("query players: %v", err)
	}
	defer rows.Close()
	var gotIDs []uuid.UUID
	var gotSlots []int
	for rows.Next() {
		var id uuid.UUID
		var slot int
		if err := rows.Scan(&id, &slot); err != nil {
			t.Fatalf("scan player: %v", err)
		}
		gotIDs = append(gotIDs, id)
		gotSlots = append(gotSlots, slot)
	}
	if len(gotIDs) != 2 || gotIDs[0] != players[0].UserID || gotIDs[1] != players[1].UserID || gotSlots[0] != 1 || gotSlots[1] != 2 {
		t.Fatalf("players = %v, slots = %v", gotIDs, gotSlots)
	}
}

func TestCreateMatchRollbackIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	ctx := context.Background()
	players := testPlayers()
	missingID := uuid.New()
	players[1].UserID = missingID
	players[1].PresenceKey = fmt.Sprintf("codeduel:presence:%s:%s", missingID, uuid.New())
	players[1].Route = "codeduel:user:" + missingID.String()

	if _, err := createMatch(ctx, pool, time.Minute, players); err == nil {
		t.Fatal("createMatch returned nil error")
	}
	var matches int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM matches`).Scan(&matches); err != nil {
		t.Fatalf("count matches: %v", err)
	}
	if matches != 0 {
		t.Fatalf("matches = %d, want 0 after rollback", matches)
	}
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
