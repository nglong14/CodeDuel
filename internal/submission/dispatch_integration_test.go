package submission

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/nglong14/CodeDuel/internal/redisx"
)

func TestImmediateDispatchIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	rdb := integrationDispatchRedis(t)
	ctx := context.Background()
	matchID := createMatch(t, pool, "active", time.Hour, integrationPlayerOne)
	dispatcher, err := NewDispatcher(pool, redisx.NewJudgeQueue(rdb), time.Minute, DefaultDispatchBatchSize)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	service := NewServiceWithDispatcher(pool, dispatcher)

	id, err := service.Accept(ctx, dispatchRequest(matchID))
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if got := rdb.XLen(ctx, redisx.JudgeJobsKey).Val(); got != 1 {
		t.Fatalf("stream length = %d, want 1", got)
	}
	var enqueuedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT last_enqueued_at FROM submissions WHERE id = $1`, id).Scan(&enqueuedAt); err != nil {
		t.Fatalf("query submission: %v", err)
	}
	if enqueuedAt == nil {
		t.Fatal("last_enqueued_at is NULL after successful XADD")
	}
}

func TestDispatchRecoveryAfterRedisFailureIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	rdb := integrationDispatchRedis(t)
	ctx := context.Background()
	matchID := createMatch(t, pool, "active", time.Hour, integrationPlayerOne)
	failing, err := NewDispatcher(pool, failingJobQueue{}, time.Minute, DefaultDispatchBatchSize)
	if err != nil {
		t.Fatalf("NewDispatcher failing: %v", err)
	}
	service := NewServiceWithDispatcher(pool, failing)

	id, err := service.Accept(ctx, dispatchRequest(matchID))
	if err != nil {
		t.Fatalf("Accept during Redis failure: %v", err)
	}
	var enqueuedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT last_enqueued_at FROM submissions WHERE id = $1`, id).Scan(&enqueuedAt); err != nil {
		t.Fatalf("query durable submission: %v", err)
	}
	if enqueuedAt != nil {
		t.Fatal("last_enqueued_at was set despite failed XADD")
	}

	recovered, err := NewDispatcher(pool, redisx.NewJudgeQueue(rdb), time.Minute, DefaultDispatchBatchSize)
	if err != nil {
		t.Fatalf("NewDispatcher recovered: %v", err)
	}
	count, err := recovered.DispatchPending(ctx)
	if err != nil {
		t.Fatalf("DispatchPending: %v", err)
	}
	if count != 1 || rdb.XLen(ctx, redisx.JudgeJobsKey).Val() != 1 {
		t.Fatalf("recovery dispatched %d jobs, stream length %d", count, rdb.XLen(ctx, redisx.JudgeJobsKey).Val())
	}
}

func TestDispatchPendingConcurrentAndDuplicateIntegration(t *testing.T) {
	pool := integrationPostgres(t)
	rdb := integrationDispatchRedis(t)
	ctx := context.Background()
	matchID := createMatch(t, pool, "active", time.Hour, integrationPlayerOne)
	service := NewService(pool)
	ids := []uuid.UUID{}
	for range 2 {
		id, err := service.Accept(ctx, dispatchRequest(matchID))
		if err != nil {
			t.Fatalf("Accept: %v", err)
		}
		ids = append(ids, id)
	}
	dispatcher, err := NewDispatcher(pool, redisx.NewJudgeQueue(rdb), time.Hour, DefaultDispatchBatchSize)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, dispatchErr := dispatcher.DispatchPending(ctx)
			errs <- dispatchErr
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent DispatchPending: %v", err)
		}
	}
	if got := rdb.XLen(ctx, redisx.JudgeJobsKey).Val(); got != 2 {
		t.Fatalf("stream length after concurrent dispatch = %d, want 2", got)
	}

	if _, err := pool.Exec(ctx, `UPDATE submissions SET last_enqueued_at = now() - interval '2 hours' WHERE id = $1`, ids[0]); err != nil {
		t.Fatalf("make dispatch stale: %v", err)
	}
	count, err := dispatcher.DispatchPending(ctx)
	if err != nil {
		t.Fatalf("DispatchPending stale row: %v", err)
	}
	if count != 1 || rdb.XLen(ctx, redisx.JudgeJobsKey).Val() != 3 {
		t.Fatalf("duplicate dispatch count = %d, stream length = %d", count, rdb.XLen(ctx, redisx.JudgeJobsKey).Val())
	}
}

type failingJobQueue struct{}

func (failingJobQueue) Enqueue(context.Context, uuid.UUID) (string, error) {
	return "", errors.New("Redis unavailable")
}

func dispatchRequest(matchID uuid.UUID) Request {
	return Request{
		PlayerID:  integrationPlayerOne,
		MatchID:   matchID,
		RequestID: uuid.New(),
		Language:  "python",
		Code:      "print(1)",
	}
}

func integrationDispatchRedis(t *testing.T) *redis.Client {
	t.Helper()
	if os.Getenv("CODEDUEL_INTEGRATION") != "1" {
		t.Skip("set CODEDUEL_INTEGRATION=1 to run integration tests")
	}
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 13})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		_ = rdb.Close()
		t.Fatalf("connect to integration Redis: %v", err)
	}
	if err := rdb.FlushDB(context.Background()).Err(); err != nil {
		_ = rdb.Close()
		t.Fatalf("flush integration Redis: %v", err)
	}
	t.Cleanup(func() {
		_ = rdb.FlushDB(context.Background()).Err()
		_ = rdb.Close()
	})
	return rdb
}
