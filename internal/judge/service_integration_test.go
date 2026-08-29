package judge

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nglong14/CodeDuel/internal/proto"
	"github.com/nglong14/CodeDuel/internal/redisx"
	"github.com/redis/go-redis/v9"
)

func TestJudgeServiceIntegration(t *testing.T) {
	pool := judgeStoreIntegrationPostgres(t)
	rdb := judgeServiceIntegrationRedis(t)
	store, err := newPostgresStore(pool)
	if err != nil {
		t.Fatalf("newPostgresStore: %v", err)
	}
	ctx := context.Background()

	t.Run("pass persists winner publishes and reconstructs duplicate", func(t *testing.T) {
		judgeServiceFlushRedis(t, rdb)
		fixture := judgeStoreIntegrationFixtureForTest(t, pool, "active")
		queue := redisx.NewJudgeQueue(rdb)
		if err := queue.EnsureGroup(ctx); err != nil {
			t.Fatalf("EnsureGroup: %v", err)
		}
		if _, err := queue.Enqueue(ctx, fixture.submissionID); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}

		var mu sync.Mutex
		var published []resultEvent
		executeCalls := 0
		service, err := newJudgeService(
			slog.New(slog.DiscardHandler),
			queue,
			store,
			executorFunc(func(context.Context, ExecutionRequest) (ExecutionOutcome, error) {
				executeCalls++
				return ExecutionOutcome{Kind: OutcomePass, TestsPassed: 3}, nil
			}),
			func(_ context.Context, channel string, payload []byte) error {
				mu.Lock()
				defer mu.Unlock()
				published = append(published, resultEvent{
					RecipientID: userIDFromChannel(t, channel),
					Payload:     bytes.Clone(payload),
				})
				return nil
			},
			"integration-consumer",
			time.Minute,
			testLimits(),
		)
		if err != nil {
			t.Fatalf("newJudgeService: %v", err)
		}

		job := readOneJudgeJob(t, queue, "integration-consumer")
		if err := service.processJob(ctx, job); err != nil {
			t.Fatalf("processJob: %v", err)
		}
		if executeCalls != 1 || len(published) != 2 {
			t.Fatalf("execute calls = %d, published events = %d", executeCalls, len(published))
		}
		for index, event := range published {
			data := decodeResultEvent(t, event.Payload)
			if event.RecipientID != fixture.players[index] || data.Verdict != proto.VerdictPass ||
				data.WinnerID != fixture.players[0].String() || data.TotalTests != 3 {
				t.Fatalf("published event %d = recipient %s, data %#v", index, event.RecipientID, data)
			}
		}
		assertCompletedJudgeState(t, pool, fixture.submissionID, fixture.matchID, "pass", fixture.players[0])
		assertJudgeQueueEmpty(t, rdb)

		firstPayloads := [][]byte{bytes.Clone(published[0].Payload), bytes.Clone(published[1].Payload)}
		if _, err := queue.Enqueue(ctx, fixture.submissionID); err != nil {
			t.Fatalf("Enqueue completed duplicate: %v", err)
		}
		duplicate := readOneJudgeJob(t, queue, "integration-consumer")
		if err := service.processJob(ctx, duplicate); err != nil {
			t.Fatalf("process completed duplicate: %v", err)
		}
		if executeCalls != 1 || len(published) != 4 {
			t.Fatalf("after duplicate, execute calls = %d, published events = %d", executeCalls, len(published))
		}
		if !bytes.Equal(firstPayloads[0], published[2].Payload) || !bytes.Equal(firstPayloads[1], published[3].Payload) {
			t.Fatal("completed duplicate did not reconstruct identical result payloads")
		}
		assertJudgeQueueEmpty(t, rdb)
	})

	t.Run("wrong answer persists and publishes only to submitter", func(t *testing.T) {
		judgeServiceFlushRedis(t, rdb)
		fixture := judgeStoreIntegrationFixtureForTest(t, pool, "active")
		queue := redisx.NewJudgeQueue(rdb)
		if err := queue.EnsureGroup(ctx); err != nil {
			t.Fatalf("EnsureGroup: %v", err)
		}
		if _, err := queue.Enqueue(ctx, fixture.submissionID); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}

		var published []resultEvent
		service, err := newJudgeService(
			slog.New(slog.DiscardHandler),
			queue,
			store,
			executorFunc(func(context.Context, ExecutionRequest) (ExecutionOutcome, error) {
				return ExecutionOutcome{Kind: OutcomeWrongAnswer, TestsPassed: 1}, nil
			}),
			func(_ context.Context, channel string, payload []byte) error {
				published = append(published, resultEvent{RecipientID: userIDFromChannel(t, channel), Payload: bytes.Clone(payload)})
				return nil
			},
			"integration-consumer",
			time.Minute,
			testLimits(),
		)
		if err != nil {
			t.Fatalf("newJudgeService: %v", err)
		}
		if err := service.processJob(ctx, readOneJudgeJob(t, queue, "integration-consumer")); err != nil {
			t.Fatalf("processJob: %v", err)
		}
		if len(published) != 1 || published[0].RecipientID != fixture.players[0] {
			t.Fatalf("published events = %#v", published)
		}
		data := decodeResultEvent(t, published[0].Payload)
		if data.Verdict != proto.VerdictFail || data.TestsPassed != 1 || data.WinnerID != "" || data.Outcome != "" {
			t.Fatalf("wrong-answer result = %#v", data)
		}
		assertCompletedJudgeState(t, pool, fixture.submissionID, fixture.matchID, "fail", uuid.Nil)
		assertJudgeQueueEmpty(t, rdb)
	})
}

func judgeServiceIntegrationRedis(t *testing.T) *redis.Client {
	t.Helper()
	if os.Getenv("CODEDUEL_INTEGRATION") != "1" {
		t.Skip("set CODEDUEL_INTEGRATION=1 to run integration tests")
	}
	address := os.Getenv("REDIS_TEST_ADDR")
	if address == "" {
		address = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: address, DB: 13})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		_ = rdb.Close()
		t.Fatalf("connect to integration Redis: %v", err)
	}
	judgeServiceFlushRedis(t, rdb)
	t.Cleanup(func() {
		_ = rdb.FlushDB(context.Background()).Err()
		_ = rdb.Close()
	})
	return rdb
}

func judgeServiceFlushRedis(t *testing.T, rdb *redis.Client) {
	t.Helper()
	if err := rdb.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush integration Redis: %v", err)
	}
}

func readOneJudgeJob(t *testing.T, queue *redisx.JudgeQueue, consumer string) redisx.JudgeJob {
	t.Helper()
	jobs, err := queue.Read(context.Background(), consumer, 1, time.Second)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(jobs) != 1 || jobs[0].DecodeErr != nil {
		t.Fatalf("jobs = %#v", jobs)
	}
	return jobs[0]
}

func assertCompletedJudgeState(
	t *testing.T,
	pool *pgxpool.Pool,
	submissionID, matchID uuid.UUID,
	verdict string,
	winnerID uuid.UUID,
) {
	t.Helper()
	var submissionStatus, storedVerdict, matchStatus string
	var storedWinner uuid.NullUUID
	if err := pool.QueryRow(context.Background(), `
		SELECT s.status, s.result, m.status, m.winner_id
		FROM submissions s
		JOIN matches m ON m.id = s.match_id
		WHERE s.id = $1 AND m.id = $2
	`, submissionID, matchID).Scan(&submissionStatus, &storedVerdict, &matchStatus, &storedWinner); err != nil {
		t.Fatalf("query completed Judge state: %v", err)
	}
	if submissionStatus != "completed" || storedVerdict != verdict {
		t.Fatalf("submission state = %q/%q", submissionStatus, storedVerdict)
	}
	if winnerID == uuid.Nil {
		if matchStatus != "active" || storedWinner.Valid {
			t.Fatalf("non-winning match state = %q/%v", matchStatus, storedWinner)
		}
	} else if matchStatus != "finished" || !storedWinner.Valid || storedWinner.UUID != winnerID {
		t.Fatalf("winning match state = %q/%v, want winner %s", matchStatus, storedWinner, winnerID)
	}
}

func assertJudgeQueueEmpty(t *testing.T, rdb *redis.Client) {
	t.Helper()
	pending, err := rdb.XPending(context.Background(), redisx.JudgeJobsKey, redisx.JudgeConsumerGroup).Result()
	if err != nil {
		t.Fatalf("XPENDING: %v", err)
	}
	if pending.Count != 0 || rdb.XLen(context.Background(), redisx.JudgeJobsKey).Val() != 0 {
		t.Fatalf("pending = %d, stream length = %d", pending.Count, rdb.XLen(context.Background(), redisx.JudgeJobsKey).Val())
	}
}

func userIDFromChannel(t *testing.T, channel string) uuid.UUID {
	t.Helper()
	const prefix = "codeduel:user:"
	if len(channel) <= len(prefix) || channel[:len(prefix)] != prefix {
		t.Fatalf("unexpected user channel %q", channel)
	}
	id, err := uuid.Parse(channel[len(prefix):])
	if err != nil {
		t.Fatalf("parse user channel %q: %v", channel, err)
	}
	return id
}
