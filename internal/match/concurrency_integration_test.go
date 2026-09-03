package match

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/nglong14/CodeDuel/internal/redisx"
)

func TestConcurrentMatchServicesIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	ctx := context.Background()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 14})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("connect to integration Redis: %v", err)
	}
	t.Cleanup(func() {
		_ = rdb.FlushDB(context.Background()).Err()
		_ = rdb.Close()
	})
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush integration Redis: %v", err)
	}

	const playerCount = 20
	queue := redisx.NewQueue(rdb, redisx.DefaultScanLimit)
	for range playerCount {
		userID := uuid.New()
		if _, err := pool.Exec(ctx, `
			INSERT INTO users (id, email, display_name)
			VALUES ($1, $2::text || '@match.test', $2::text)
		`, userID, userID.String()); err != nil {
			t.Fatalf("insert user: %v", err)
		}
		member := redisx.QueueMember{
			UserID:      userID,
			PresenceKey: redisx.PresenceKey(userID, uuid.New()),
			Route:       redisx.UserChannel(userID),
		}
		if err := rdb.Set(ctx, member.PresenceKey, "1", time.Minute).Err(); err != nil {
			t.Fatalf("set presence: %v", err)
		}
		if _, err := queue.Enqueue(ctx, member); err != nil {
			t.Fatalf("enqueue user: %v", err)
		}
	}

	newReplica := func() *service {
		replica, err := newService(
			slog.New(slog.DiscardHandler),
			redisx.NewQueue(rdb, redisx.DefaultScanLimit),
			func(callCtx context.Context, players [2]redisx.QueueMember) (CreatedMatch, error) {
				return createMatch(callCtx, pool, time.Minute, players)
			},
			func(callCtx context.Context, route string, payload []byte) error {
				return rdb.Publish(callCtx, route, payload).Err()
			},
		)
		if err != nil {
			t.Fatalf("newService: %v", err)
		}
		replica.pollInterval = 5 * time.Millisecond
		replica.retryInterval = 10 * time.Millisecond
		return replica
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 2)
	for range 2 {
		replica := newReplica()
		go func() { done <- replica.run(runCtx) }()
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var matches int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM matches`).Scan(&matches); err != nil {
			cancel()
			t.Fatalf("count matches: %v", err)
		}
		if matches == playerCount/2 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("matches = %d, want %d", matches, playerCount/2)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	for range 2 {
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("service error = %v", err)
		}
	}

	var playerRows, distinctPlayers, invalidMatches int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(DISTINCT user_id) FROM match_players`).Scan(&playerRows, &distinctPlayers); err != nil {
		t.Fatalf("count match players: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT match_id FROM match_players GROUP BY match_id HAVING count(*) <> 2
		) invalid
	`).Scan(&invalidMatches); err != nil {
		t.Fatalf("count invalid matches: %v", err)
	}
	if playerRows != playerCount || distinctPlayers != playerCount || invalidMatches != 0 {
		t.Fatalf("player rows = %d, distinct = %d, invalid matches = %d", playerRows, distinctPlayers, invalidMatches)
	}
	if got := rdb.ZCard(ctx, redisx.QueueKey).Val(); got != 0 {
		t.Fatalf("queue size = %d, want 0", got)
	}
}
