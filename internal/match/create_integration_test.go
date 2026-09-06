package match

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nglong14/CodeDuel/internal/infrastructure"
	"github.com/nglong14/CodeDuel/internal/redisx"
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
	var claims int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM active_match_players WHERE match_id = $1`, created.MatchID).Scan(&claims); err != nil {
		t.Fatalf("count active claims: %v", err)
	}
	if claims != 2 {
		t.Fatalf("active claims = %d, want 2", claims)
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
	} else {
		var missingErr *MissingPlayersError
		if !errors.As(err, &missingErr) || !missingErr.Has(missingID) {
			t.Fatalf("createMatch error = %v, want missing player %s", err, missingID)
		}
	}
	var matches int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM matches`).Scan(&matches); err != nil {
		t.Fatalf("count matches: %v", err)
	}
	if matches != 0 {
		t.Fatalf("matches = %d, want 0 after rollback", matches)
	}
}

func TestCreateMatchRejectsAndReleasesActivePlayersIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	ctx := context.Background()
	players := testPlayers()
	first, err := createMatch(ctx, pool, time.Minute, players)
	if err != nil {
		t.Fatalf("create first match: %v", err)
	}

	thirdID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, display_name)
		VALUES ($1, $2::text || '@match.test', $2::text)
	`, thirdID, thirdID.String()); err != nil {
		t.Fatalf("insert third user: %v", err)
	}
	secondPlayers := players
	secondPlayers[1].UserID = thirdID
	secondPlayers[1].PresenceKey = redisx.PresenceKey(thirdID, uuid.New())
	secondPlayers[1].Route = redisx.UserChannel(thirdID)

	if _, err := createMatch(ctx, pool, time.Minute, secondPlayers); err == nil {
		t.Fatal("create overlapping match returned nil error")
	} else {
		var activeErr *ActiveMatchConflictError
		if !errors.As(err, &activeErr) {
			t.Fatalf("create overlapping match error = %v, want active conflict", err)
		}
		if matchID, ok := activeErr.MatchFor(players[0].UserID); !ok || matchID != first.MatchID {
			t.Fatalf("active claim = (%s, %v), want %s", matchID, ok, first.MatchID)
		}
	}

	if _, err := pool.Exec(ctx, `UPDATE matches SET status = 'finished' WHERE id = $1`, first.MatchID); err != nil {
		t.Fatalf("finish first match: %v", err)
	}
	var claims int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM active_match_players WHERE match_id = $1`, first.MatchID).Scan(&claims); err != nil {
		t.Fatalf("count released claims: %v", err)
	}
	if claims != 0 {
		t.Fatalf("released claims = %d, want 0", claims)
	}
	if _, err := createMatch(ctx, pool, time.Minute, secondPlayers); err != nil {
		t.Fatalf("create match after release: %v", err)
	}
}

func TestActiveMatchClaimConstraintIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	ctx := context.Background()
	players := testPlayers()
	if _, err := createMatch(ctx, pool, time.Minute, players); err != nil {
		t.Fatalf("create first match: %v", err)
	}

	var secondMatchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO matches (problem_id, status, deadline)
		SELECT id, 'active', clock_timestamp() + interval '1 hour'
		FROM problems
		ORDER BY created_at, id
		LIMIT 1
		RETURNING id
	`).Scan(&secondMatchID); err != nil {
		t.Fatalf("insert second match: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO match_players (match_id, user_id, slot)
		VALUES ($1, $2, 1)
	`, secondMatchID, players[0].UserID); err == nil {
		t.Fatal("database accepted a second active match for one player")
	}
}

func TestActiveMatchClaimTriggersSerializeMembershipAndStatusIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	ctx := context.Background()

	t.Run("concurrent memberships use compatible locks", func(t *testing.T) {
		firstUserID := insertMatchTestUser(t, pool)
		secondUserID := insertMatchTestUser(t, pool)
		matchID := insertMatchTestMatch(t, pool, "active")
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin first membership transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `INSERT INTO match_players (match_id, user_id, slot) VALUES ($1, $2, 1)`, matchID, firstUserID); err != nil {
			t.Fatalf("insert first membership: %v", err)
		}

		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire second membership connection: %v", err)
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx, `SET lock_timeout = '100ms'`); err != nil {
			t.Fatalf("set lock timeout: %v", err)
		}
		defer func() { _, _ = conn.Exec(context.Background(), `SET lock_timeout = 0`) }()
		if _, err := conn.Exec(ctx, `INSERT INTO match_players (match_id, user_id, slot) VALUES ($1, $2, 2)`, matchID, secondUserID); err != nil {
			t.Fatalf("insert concurrent membership: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit first membership: %v", err)
		}
		assertActiveClaimCount(t, pool, matchID, 2)
	})

	t.Run("membership before finish releases claim", func(t *testing.T) {
		userID := insertMatchTestUser(t, pool)
		matchID := insertMatchTestMatch(t, pool, "active")
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin membership transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `INSERT INTO match_players (match_id, user_id, slot) VALUES ($1, $2, 1)`, matchID, userID); err != nil {
			t.Fatalf("insert membership: %v", err)
		}

		assertMatchStatusUpdateBlocked(t, pool, matchID, "finished")
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit membership: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE matches SET status = 'finished' WHERE id = $1`, matchID); err != nil {
			t.Fatalf("finish match: %v", err)
		}
		assertActiveClaimCount(t, pool, matchID, 0)
	})

	t.Run("membership before reactivation creates claim", func(t *testing.T) {
		userID := insertMatchTestUser(t, pool)
		matchID := insertMatchTestMatch(t, pool, "finished")
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin membership transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `INSERT INTO match_players (match_id, user_id, slot) VALUES ($1, $2, 1)`, matchID, userID); err != nil {
			t.Fatalf("insert membership: %v", err)
		}

		assertMatchStatusUpdateBlocked(t, pool, matchID, "active")
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit membership: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE matches SET status = 'active' WHERE id = $1`, matchID); err != nil {
			t.Fatalf("reactivate match: %v", err)
		}
		assertActiveClaimCount(t, pool, matchID, 1)
	})
}

func insertMatchTestUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, email, display_name)
		VALUES ($1, $2::text || '@match.test', $2::text)
	`, userID, userID.String()); err != nil {
		t.Fatalf("insert match test user: %v", err)
	}
	return userID
}

func insertMatchTestMatch(t *testing.T, pool *pgxpool.Pool, status string) uuid.UUID {
	t.Helper()
	var matchID uuid.UUID
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO matches (problem_id, status, deadline)
		SELECT id, $1, clock_timestamp() + interval '1 hour'
		FROM problems
		ORDER BY created_at, id
		LIMIT 1
		RETURNING id
	`, status).Scan(&matchID); err != nil {
		t.Fatalf("insert match test match: %v", err)
	}
	return matchID
}

func assertMatchStatusUpdateBlocked(t *testing.T, pool *pgxpool.Pool, matchID uuid.UUID, status string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire status update connection: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET lock_timeout = '100ms'`); err != nil {
		t.Fatalf("set lock timeout: %v", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SET lock_timeout = 0`) }()
	_, err = conn.Exec(ctx, `UPDATE matches SET status = $2 WHERE id = $1`, matchID, status)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("status update error = %v, want lock timeout", err)
	}
}

func assertActiveClaimCount(t *testing.T, pool *pgxpool.Pool, matchID uuid.UUID, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM active_match_players WHERE match_id = $1`, matchID).Scan(&got); err != nil {
		t.Fatalf("count active claims: %v", err)
	}
	if got != want {
		t.Fatalf("active claims = %d, want %d", got, want)
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
