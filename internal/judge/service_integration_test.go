package judge

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
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

	t.Run("concurrent duplicate entries execute and complete once", func(t *testing.T) {
		judgeServiceFlushRedis(t, rdb)
		fixture := judgeStoreIntegrationFixtureForTest(t, pool, "active")
		queue := redisx.NewJudgeQueue(rdb)
		if err := queue.EnsureGroup(ctx); err != nil {
			t.Fatalf("EnsureGroup: %v", err)
		}
		firstEntryID, err := queue.Enqueue(ctx, fixture.submissionID)
		if err != nil {
			t.Fatalf("Enqueue first duplicate: %v", err)
		}
		secondEntryID, err := queue.Enqueue(ctx, fixture.submissionID)
		if err != nil {
			t.Fatalf("Enqueue second duplicate: %v", err)
		}
		firstJob := readOneJudgeJob(t, queue, "duplicate-consumer-one")
		secondJob := readOneJudgeJob(t, queue, "duplicate-consumer-two")
		if firstJob.EntryID != firstEntryID || secondJob.EntryID != secondEntryID {
			t.Fatalf("read entry IDs = %q/%q, want %q/%q", firstJob.EntryID, secondJob.EntryID, firstEntryID, secondEntryID)
		}

		var executeCalls atomic.Int32
		executorStarted := make(chan struct{})
		releaseExecutor := make(chan struct{})
		var releaseOnce sync.Once
		release := func() {
			releaseOnce.Do(func() { close(releaseExecutor) })
		}
		defer release()
		executor := executorFunc(func(executeCtx context.Context, _ ExecutionRequest) (ExecutionOutcome, error) {
			if executeCalls.Add(1) == 1 {
				close(executorStarted)
				select {
				case <-releaseExecutor:
				case <-executeCtx.Done():
					return ExecutionOutcome{}, executeCtx.Err()
				}
			}
			return ExecutionOutcome{Kind: OutcomeWrongAnswer, TestsPassed: 1}, nil
		})

		var publishMu sync.Mutex
		var publishedChannels []string
		var publishedPayloads [][]byte
		publish := func(_ context.Context, channel string, payload []byte) error {
			publishMu.Lock()
			defer publishMu.Unlock()
			publishedChannels = append(publishedChannels, channel)
			publishedPayloads = append(publishedPayloads, bytes.Clone(payload))
			return nil
		}
		newService := func(consumer string) *judgeService {
			service, err := newJudgeService(
				slog.New(slog.DiscardHandler), queue, store, executor, publish,
				consumer, time.Minute, testLimits(),
			)
			if err != nil {
				t.Fatalf("newJudgeService %q: %v", consumer, err)
			}
			return service
		}
		firstService := newService("duplicate-consumer-one")
		secondService := newService("duplicate-consumer-two")

		firstDone := make(chan error, 1)
		go func() {
			firstDone <- firstService.processJob(ctx, firstJob)
		}()
		select {
		case <-executorStarted:
		case err := <-firstDone:
			t.Fatalf("first processJob finished before executor blocked: %v", err)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for first executor")
		}

		secondDone := make(chan error, 1)
		go func() {
			secondDone <- secondService.processJob(ctx, secondJob)
		}()
		secondErr := receiveJudgeTest(t, secondDone, "duplicate job completion")
		release()
		firstErr := receiveJudgeTest(t, firstDone, "original job completion")
		if firstErr != nil || secondErr != nil {
			t.Fatalf("processJob errors = first %v, second %v", firstErr, secondErr)
		}
		if calls := executeCalls.Load(); calls != 1 {
			t.Fatalf("executor calls = %d, want 1", calls)
		}

		publishMu.Lock()
		channels := append([]string(nil), publishedChannels...)
		payloads := append([][]byte(nil), publishedPayloads...)
		publishMu.Unlock()
		if len(channels) != 1 || channels[0] != redisx.UserChannel(fixture.players[0]) || len(payloads) != 1 {
			t.Fatalf("published channels/payloads = %v/%d", channels, len(payloads))
		}
		data := decodeResultEvent(t, payloads[0])
		if data.SubmissionID != fixture.submissionID.String() || data.Verdict != proto.VerdictFail ||
			data.TestsPassed != 1 || data.TotalTests != len(judgeStoreIntegrationTests()) {
			t.Fatalf("terminal result event = %#v", data)
		}

		var attempts, testsPassed int
		var failureKind string
		if err := pool.QueryRow(ctx, `
			SELECT attempts, tests_passed, failure_kind
			FROM submissions
			WHERE id = $1
		`, fixture.submissionID).Scan(&attempts, &testsPassed, &failureKind); err != nil {
			t.Fatalf("query duplicate submission result: %v", err)
		}
		if attempts != 1 || testsPassed != 1 || failureKind != "wrong_answer" {
			t.Fatalf("attempts/tests/failure = %d/%d/%q", attempts, testsPassed, failureKind)
		}
		assertCompletedJudgeState(t, pool, fixture.submissionID, fixture.matchID, "fail", uuid.Nil)
		assertJudgeQueueEmpty(t, rdb)
	})

	t.Run("publication failure leaves completed entry pending", func(t *testing.T) {
		judgeServiceFlushRedis(t, rdb)
		fixture := judgeStoreIntegrationFixtureForTest(t, pool, "active")
		queue := redisx.NewJudgeQueue(rdb)
		if err := queue.EnsureGroup(ctx); err != nil {
			t.Fatalf("EnsureGroup: %v", err)
		}
		entryID, err := queue.Enqueue(ctx, fixture.submissionID)
		if err != nil {
			t.Fatalf("Enqueue: %v", err)
		}

		executeCalls := 0
		publishCalls := 0
		publishErr := errors.New("result publication unavailable")
		service, err := newJudgeService(
			slog.New(slog.DiscardHandler),
			queue,
			store,
			executorFunc(func(context.Context, ExecutionRequest) (ExecutionOutcome, error) {
				executeCalls++
				return ExecutionOutcome{Kind: OutcomeWrongAnswer, TestsPassed: 2}, nil
			}),
			func(context.Context, string, []byte) error {
				publishCalls++
				return publishErr
			},
			"publication-failure-consumer",
			time.Minute,
			testLimits(),
		)
		if err != nil {
			t.Fatalf("newJudgeService: %v", err)
		}
		job := readOneJudgeJob(t, queue, "publication-failure-consumer")
		if job.EntryID != entryID {
			t.Fatalf("read entry ID = %q, want %q", job.EntryID, entryID)
		}
		if err := service.processJob(ctx, job); !errors.Is(err, publishErr) {
			t.Fatalf("processJob error = %v, want publication failure", err)
		}
		if executeCalls != 1 || publishCalls != 1 {
			t.Fatalf("execute/publish calls = %d/%d, want 1/1", executeCalls, publishCalls)
		}

		pending, err := rdb.XPending(ctx, redisx.JudgeJobsKey, redisx.JudgeConsumerGroup).Result()
		if err != nil {
			t.Fatalf("XPENDING: %v", err)
		}
		if pending.Count != 1 {
			t.Fatalf("XPENDING count = %d, want 1", pending.Count)
		}
		pendingEntries, err := rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: redisx.JudgeJobsKey,
			Group:  redisx.JudgeConsumerGroup,
			Start:  "-",
			End:    "+",
			Count:  2,
		}).Result()
		if err != nil {
			t.Fatalf("XPENDING range: %v", err)
		}
		if len(pendingEntries) != 1 || pendingEntries[0].ID != entryID || pendingEntries[0].Consumer != "publication-failure-consumer" {
			t.Fatalf("pending entries = %#v, want entry %q", pendingEntries, entryID)
		}
		streamEntries, err := rdb.XRange(ctx, redisx.JudgeJobsKey, entryID, entryID).Result()
		if err != nil {
			t.Fatalf("XRANGE pending entry: %v", err)
		}
		if len(streamEntries) != 1 || streamEntries[0].ID != entryID {
			t.Fatalf("stream entries = %#v, want undeleted entry %q", streamEntries, entryID)
		}

		var attempts, testsPassed int
		var failureKind string
		if err := pool.QueryRow(ctx, `
			SELECT attempts, tests_passed, failure_kind
			FROM submissions
			WHERE id = $1
		`, fixture.submissionID).Scan(&attempts, &testsPassed, &failureKind); err != nil {
			t.Fatalf("query publication-failure submission result: %v", err)
		}
		if attempts != 1 || testsPassed != 2 || failureKind != "wrong_answer" {
			t.Fatalf("attempts/tests/failure = %d/%d/%q", attempts, testsPassed, failureKind)
		}
		assertCompletedJudgeState(t, pool, fixture.submissionID, fixture.matchID, "fail", uuid.Nil)
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
	rdb := redis.NewClient(&redis.Options{Addr: address, DB: 12})
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
