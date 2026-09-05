package reaper

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/nglong14/CodeDuel/internal/config"
	"github.com/nglong14/CodeDuel/internal/infrastructure"
	"github.com/nglong14/CodeDuel/internal/proto"
	"github.com/nglong14/CodeDuel/internal/redisx"
	"github.com/nglong14/CodeDuel/internal/submission"
)

func TestReaperLeaseReclaimIntegration(t *testing.T) {
	pool := reaperIntegrationPostgres(t)
	svc, published := newReaperIntegrationService(t, pool, nil, time.Hour)

	t.Run("expired lease under cap returns to pending", func(t *testing.T) {
		fixture := reaperIntegrationFixture(t, pool, reaperFixtureOpts{
			MatchStatus:      "active",
			Deadline:         time.Hour,
			SubmissionStatus: "running",
			Attempts:         1,
			LeaseAge:         -time.Second,
		})
		if err := svc.tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
		assertSubmissionLifecycle(t, pool, fixture.submissionID, "pending", nil, nil, 1)
		if published.count() != 0 {
			t.Fatalf("published %d events, want 0", published.count())
		}
	})

	t.Run("live lease is untouched", func(t *testing.T) {
		published.reset()
		fixture := reaperIntegrationFixture(t, pool, reaperFixtureOpts{
			MatchStatus:      "active",
			Deadline:         time.Hour,
			SubmissionStatus: "running",
			Attempts:         1,
			LeaseAge:         time.Hour,
		})
		if err := svc.tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
		status, token, leaseUntil := loadSubmissionLease(t, pool, fixture.submissionID)
		if status != "running" || !token.Valid || leaseUntil == nil {
			t.Fatalf("live lease mutated: status=%s token=%v lease=%v", status, token, leaseUntil)
		}
	})

	t.Run("attempts at the cap become failed and notify submitter", func(t *testing.T) {
		published.reset()
		fixture := reaperIntegrationFixture(t, pool, reaperFixtureOpts{
			MatchStatus:      "active",
			Deadline:         time.Hour,
			SubmissionStatus: "running",
			Attempts:         3,
			LeaseAge:         -time.Second,
		})
		if err := svc.tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
		failed := "failed"
		kind := "infrastructure_error"
		assertSubmissionLifecycle(t, pool, fixture.submissionID, "completed", &failed, &kind, 3)
		events := published.snapshot()
		if len(events) != 1 {
			t.Fatalf("published %d events, want 1", len(events))
		}
		if events[0].channel != redisx.UserChannel(fixture.players[0]) {
			t.Fatalf("channel = %s, want submitter", events[0].channel)
		}
		envelope, err := proto.Decode(events[0].payload)
		if err != nil || envelope.Type != proto.TypeResult {
			t.Fatalf("payload type = %v %q", err, envelope.Type)
		}
		var data proto.ResultData
		if err := envelope.DecodeData(&data); err != nil {
			t.Fatalf("DecodeData: %v", err)
		}
		if data.Verdict != proto.VerdictFailed || data.SubmissionID != fixture.submissionID.String() ||
			data.WinnerID != "" || data.Outcome != "" || data.TestsPassed != 0 || data.TotalTests != 3 {
			t.Fatalf("poison result = %#v", data)
		}
	})
}

func TestReaperAdvisoryLockSkipsHeldTickIntegration(t *testing.T) {
	pool := reaperIntegrationPostgres(t)
	svc, _ := newReaperIntegrationService(t, pool, nil, time.Hour)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer conn.Release()
	var acquired bool
	if err := conn.QueryRow(ctx, advisoryLockSQL).Scan(&acquired); err != nil || !acquired {
		t.Fatalf("hold advisory lock: acquired=%v err=%v", acquired, err)
	}
	defer func() { _, _ = conn.Exec(ctx, advisoryUnlockSQL) }()

	fixture := reaperIntegrationFixture(t, pool, reaperFixtureOpts{
		MatchStatus:      "active",
		Deadline:         time.Hour,
		SubmissionStatus: "running",
		Attempts:         1,
		LeaseAge:         -time.Second,
	})
	if err := svc.tick(ctx); err != nil {
		t.Fatalf("tick while lock held: %v", err)
	}
	status, _, _ := loadSubmissionLease(t, pool, fixture.submissionID)
	if status != "running" {
		t.Fatalf("status = %q, want running while lock is held", status)
	}
}

func TestReaperStreamSweepIntegration(t *testing.T) {
	pool := reaperIntegrationPostgres(t)
	rdb := reaperIntegrationRedis(t)
	ctx := context.Background()
	queue := redisx.NewJudgeQueue(rdb)
	if err := queue.EnsureGroup(ctx); err != nil {
		t.Fatalf("EnsureGroup: %v", err)
	}

	t.Run("idle pending entry is redispatched and finalized", func(t *testing.T) {
		flushReaperRedis(t, rdb)
		if err := queue.EnsureGroup(ctx); err != nil {
			t.Fatalf("EnsureGroup: %v", err)
		}
		svc, _ := newReaperIntegrationService(t, pool, rdb, 0)
		fixture := reaperIntegrationFixture(t, pool, reaperFixtureOpts{
			MatchStatus:      "active",
			Deadline:         time.Hour,
			SubmissionStatus: "pending",
		})
		if _, err := queue.Enqueue(ctx, fixture.submissionID); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		jobs, err := queue.Read(ctx, "abandoned-judge", 1, time.Second)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("Read abandoned = %#v, %v", jobs, err)
		}
		if err := svc.tick(ctx); err != nil {
			t.Fatalf("tick: %v", err)
		}
		pending, err := rdb.XPending(ctx, redisx.JudgeJobsKey, redisx.JudgeConsumerGroup).Result()
		if err != nil {
			t.Fatalf("XPENDING: %v", err)
		}
		if pending.Count != 0 {
			t.Fatalf("pending count = %d, want 0", pending.Count)
		}
		replacement, err := queue.Read(ctx, "live-judge", 1, time.Second)
		if err != nil || len(replacement) != 1 || replacement[0].SubmissionID != fixture.submissionID {
			t.Fatalf("replacement = %#v, %v", replacement, err)
		}
		if replacement[0].EntryID == jobs[0].EntryID {
			t.Fatal("replacement reused the abandoned entry ID")
		}
	})

	t.Run("fresh pending entry is left alone", func(t *testing.T) {
		flushReaperRedis(t, rdb)
		if err := queue.EnsureGroup(ctx); err != nil {
			t.Fatalf("EnsureGroup: %v", err)
		}
		svc, _ := newReaperIntegrationService(t, pool, rdb, time.Hour)
		fixture := reaperIntegrationFixture(t, pool, reaperFixtureOpts{
			MatchStatus:      "active",
			Deadline:         time.Hour,
			SubmissionStatus: "pending",
		})
		if _, err := queue.Enqueue(ctx, fixture.submissionID); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		jobs, err := queue.Read(ctx, "fresh-judge", 1, time.Second)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("Read fresh = %#v, %v", jobs, err)
		}
		if err := svc.tick(ctx); err != nil {
			t.Fatalf("tick: %v", err)
		}
		pending, err := rdb.XPending(ctx, redisx.JudgeJobsKey, redisx.JudgeConsumerGroup).Result()
		if err != nil {
			t.Fatalf("XPENDING: %v", err)
		}
		if pending.Count != 1 {
			t.Fatalf("pending count = %d, want 1 for fresh entry", pending.Count)
		}
		if rdb.XLen(ctx, redisx.JudgeJobsKey).Val() != 1 {
			t.Fatalf("stream length = %d, want 1", rdb.XLen(ctx, redisx.JudgeJobsKey).Val())
		}
	})
}

func TestReaperMatchFinalizationIntegration(t *testing.T) {
	pool := reaperIntegrationPostgres(t)
	svc, published := newReaperIntegrationService(t, pool, nil, time.Hour)
	ctx := context.Background()

	t.Run("pending work defers deadline finalization", func(t *testing.T) {
		published.reset()
		fixture := reaperIntegrationFixture(t, pool, reaperFixtureOpts{
			MatchStatus:      "active",
			Deadline:         -time.Second,
			SubmissionStatus: "pending",
		})
		if err := svc.tick(ctx); err != nil {
			t.Fatalf("tick: %v", err)
		}
		status, winner := loadMatchState(t, pool, fixture.matchID)
		if status != "active" || winner.Valid {
			t.Fatalf("match = status %q winner %v, want still active", status, winner)
		}
		if published.count() != 0 {
			t.Fatalf("published %d events, want 0", published.count())
		}

		if _, err := pool.Exec(ctx, `
			UPDATE submissions
			SET status = 'completed',
			    result = 'fail',
			    failure_kind = 'wrong_answer',
			    tests_passed = 2,
			    finished_at = clock_timestamp()
			WHERE id = $1
		`, fixture.submissionID); err != nil {
			t.Fatalf("complete pending submission: %v", err)
		}
		if err := svc.tick(ctx); err != nil {
			t.Fatalf("tick after completion: %v", err)
		}
		status, winner = loadMatchState(t, pool, fixture.matchID)
		if status != "finished" || !winner.Valid || winner.UUID != fixture.players[0] {
			t.Fatalf("finalized match = status %q winner %v, want finished %s", status, winner, fixture.players[0])
		}
		var activeClaims int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM active_match_players WHERE match_id = $1`, fixture.matchID).Scan(&activeClaims); err != nil {
			t.Fatalf("count active claims: %v", err)
		}
		if activeClaims != 0 {
			t.Fatalf("active claims = %d, want 0 after finalization", activeClaims)
		}
		if published.count() != 2 {
			t.Fatalf("published %d match_end events, want 2", published.count())
		}
	})

	t.Run("tiebreak winner versus draw", func(t *testing.T) {
		published.reset()
		winnerFixture := reaperIntegrationFixture(t, pool, reaperFixtureOpts{
			MatchStatus:      "active",
			Deadline:         -time.Second,
			SubmissionStatus: "completed",
			Result:           "fail",
			FailureKind:      "wrong_answer",
			TestsPassed:      2,
		})
		insertCompletedSubmission(t, pool, winnerFixture, winnerFixture.players[1], 1)
		if err := svc.tick(ctx); err != nil {
			t.Fatalf("tick winner: %v", err)
		}
		status, winner := loadMatchState(t, pool, winnerFixture.matchID)
		if status != "finished" || !winner.Valid || winner.UUID != winnerFixture.players[0] {
			t.Fatalf("tiebreak winner = status %q winner %v", status, winner)
		}

		published.reset()
		drawFixture := reaperIntegrationFixture(t, pool, reaperFixtureOpts{
			MatchStatus:      "active",
			Deadline:         -time.Second,
			SubmissionStatus: "completed",
			Result:           "fail",
			FailureKind:      "wrong_answer",
			TestsPassed:      1,
		})
		insertCompletedSubmission(t, pool, drawFixture, drawFixture.players[1], 1)
		if err := svc.tick(ctx); err != nil {
			t.Fatalf("tick draw: %v", err)
		}
		status, winner = loadMatchState(t, pool, drawFixture.matchID)
		if status != "finished" || winner.Valid {
			t.Fatalf("draw = status %q winner %v, want finished NULL", status, winner)
		}
		events := published.snapshot()
		if len(events) != 2 {
			t.Fatalf("draw events = %d, want 2", len(events))
		}
		for _, event := range events {
			envelope, err := proto.Decode(event.payload)
			if err != nil || envelope.Type != proto.TypeMatchEnd {
				t.Fatalf("draw payload = %v %q", err, envelope.Type)
			}
			var data proto.MatchEndData
			if err := envelope.DecodeData(&data); err != nil {
				t.Fatalf("DecodeData: %v", err)
			}
			if data.Outcome != proto.OutcomeDraw || data.WinnerID != "" {
				t.Fatalf("draw data = %#v", data)
			}
		}
	})

	t.Run("already finished match is never rewritten", func(t *testing.T) {
		published.reset()
		fixture := reaperIntegrationFixture(t, pool, reaperFixtureOpts{
			MatchStatus:      "finished",
			Deadline:         -time.Second,
			SubmissionStatus: "completed",
			Result:           "pass",
			TestsPassed:      3,
		})
		if _, err := pool.Exec(ctx, `UPDATE matches SET winner_id = $1 WHERE id = $2`, fixture.players[0], fixture.matchID); err != nil {
			t.Fatalf("set existing winner: %v", err)
		}
		if err := svc.tick(ctx); err != nil {
			t.Fatalf("tick finished match: %v", err)
		}
		status, winner := loadMatchState(t, pool, fixture.matchID)
		if status != "finished" || !winner.Valid || winner.UUID != fixture.players[0] {
			t.Fatalf("finished match mutated: status %q winner %v", status, winner)
		}
		if published.count() != 0 {
			t.Fatalf("published %d events for already finished match", published.count())
		}
	})
}

type reaperFixtureOpts struct {
	MatchStatus      string
	Deadline         time.Duration
	SubmissionStatus string
	Attempts         int
	LeaseAge         time.Duration
	Result           string
	FailureKind      string
	TestsPassed      int
}

type reaperIntegrationFixtureState struct {
	matchID      uuid.UUID
	problemID    uuid.UUID
	submissionID uuid.UUID
	players      [2]uuid.UUID
}

type recordedPublish struct {
	channel string
	payload []byte
}

type recordingPublisher struct {
	mu     sync.Mutex
	events []recordedPublish
}

func (p *recordingPublisher) publish(_ context.Context, channel string, payload []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	copied := make([]byte, len(payload))
	copy(copied, payload)
	p.events = append(p.events, recordedPublish{channel: channel, payload: copied})
	return nil
}

func (p *recordingPublisher) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = nil
}

func (p *recordingPublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

func (p *recordingPublisher) snapshot() []recordedPublish {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]recordedPublish, len(p.events))
	copy(out, p.events)
	return out
}

func newReaperIntegrationService(
	t *testing.T,
	pool *pgxpool.Pool,
	rdb *redis.Client,
	minIdle time.Duration,
) (*service, *recordingPublisher) {
	t.Helper()
	if rdb == nil {
		rdb = reaperIntegrationRedis(t)
	}
	queue := redisx.NewJudgeQueue(rdb)
	if err := queue.EnsureGroup(context.Background()); err != nil {
		t.Fatalf("EnsureGroup: %v", err)
	}
	dispatcher, err := submission.NewDispatcher(pool, queue, time.Minute, submission.DefaultDispatchBatchSize)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	published := &recordingPublisher{}
	svc, err := newService(
		slog.New(slog.DiscardHandler),
		pool,
		queue,
		dispatcher,
		published.publish,
		"reaper-test-"+uuid.NewString(),
		config.ReaperConfig{
			Interval:      10 * time.Millisecond,
			MaxAttempts:   3,
			StreamMinIdle: minIdle,
			BatchSize:     32,
		},
	)
	if err != nil {
		t.Fatalf("newService: %v", err)
	}
	return svc, published
}

func reaperIntegrationFixture(t *testing.T, pool *pgxpool.Pool, opts reaperFixtureOpts) reaperIntegrationFixtureState {
	t.Helper()
	ctx := context.Background()
	fixture := reaperIntegrationFixtureState{players: [2]uuid.UUID{uuid.New(), uuid.New()}}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, display_name)
		VALUES ($1, $3::text || '@reaper.test', $3::text),
		       ($2, $4::text || '@reaper.test', $4::text)
	`, fixture.players[0], fixture.players[1], fixture.players[0].String(), fixture.players[1].String()); err != nil {
		t.Fatalf("insert fixture users: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO problems (title, statement, test_cases)
		VALUES ('Reaper integration problem', 'Test statement', $1::jsonb)
		RETURNING id
	`, `[
		{"input":"first input\n","expected":"first output\n"},
		{"input":"second input\n","expected":"second output\n"},
		{"input":"third input\n","expected":"third output\n"}
	]`).Scan(&fixture.problemID); err != nil {
		t.Fatalf("insert fixture problem: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO matches (problem_id, status, deadline)
		VALUES ($1, $2, clock_timestamp() + ($3 * interval '1 microsecond'))
		RETURNING id
	`, fixture.problemID, opts.MatchStatus, opts.Deadline.Microseconds()).Scan(&fixture.matchID); err != nil {
		t.Fatalf("insert fixture match: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO match_players (match_id, user_id, slot)
		VALUES ($1, $2, 1), ($1, $3, 2)
	`, fixture.matchID, fixture.players[0], fixture.players[1]); err != nil {
		t.Fatalf("insert fixture match players: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO submissions (match_id, player_id, request_id, language, code)
		VALUES ($1, $2, $3, 'python', 'print(1)')
		RETURNING id
	`, fixture.matchID, fixture.players[0], uuid.New()).Scan(&fixture.submissionID); err != nil {
		t.Fatalf("insert fixture submission: %v", err)
	}
	applySubmissionState(t, pool, fixture.submissionID, opts)
	return fixture
}

func applySubmissionState(t *testing.T, pool *pgxpool.Pool, submissionID uuid.UUID, opts reaperFixtureOpts) {
	t.Helper()
	ctx := context.Background()
	switch opts.SubmissionStatus {
	case "", "pending":
		return
	case "running":
		token := uuid.New()
		if _, err := pool.Exec(ctx, `
			UPDATE submissions
			SET status = 'running',
			    attempts = $2,
			    attempt_token = $3,
			    lease_until = clock_timestamp() + ($4 * interval '1 microsecond')
			WHERE id = $1
		`, submissionID, opts.Attempts, token, opts.LeaseAge.Microseconds()); err != nil {
			t.Fatalf("mark submission running: %v", err)
		}
	case "completed":
		var failure any
		if opts.FailureKind != "" {
			failure = opts.FailureKind
		}
		if _, err := pool.Exec(ctx, `
			UPDATE submissions
			SET status = 'completed',
			    result = $2,
			    failure_kind = $3,
			    tests_passed = $4,
			    finished_at = clock_timestamp()
			WHERE id = $1
		`, submissionID, opts.Result, failure, opts.TestsPassed); err != nil {
			t.Fatalf("mark submission completed: %v", err)
		}
	default:
		t.Fatalf("unknown fixture submission status %q", opts.SubmissionStatus)
	}
}

func insertCompletedSubmission(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture reaperIntegrationFixtureState,
	playerID uuid.UUID,
	testsPassed int,
) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO submissions (
			match_id, player_id, request_id, language, code,
			status, result, failure_kind, tests_passed, finished_at
		)
		VALUES ($1, $2, $3, 'python', 'print(2)', 'completed', 'fail', 'wrong_answer', $4, clock_timestamp())
	`, fixture.matchID, playerID, uuid.New(), testsPassed); err != nil {
		t.Fatalf("insert completed submission: %v", err)
	}
}

func assertSubmissionLifecycle(
	t *testing.T,
	pool *pgxpool.Pool,
	id uuid.UUID,
	wantStatus string,
	wantResult, wantFailure *string,
	wantAttempts int,
) {
	t.Helper()
	var status string
	var result, failure *string
	var attempts int
	var token uuid.NullUUID
	var leaseUntil, finishedAt *time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT status, result, failure_kind, attempts, attempt_token, lease_until, finished_at
		FROM submissions
		WHERE id = $1
	`, id).Scan(&status, &result, &failure, &attempts, &token, &leaseUntil, &finishedAt); err != nil {
		t.Fatalf("query submission lifecycle: %v", err)
	}
	if status != wantStatus || attempts != wantAttempts || token.Valid || leaseUntil != nil {
		t.Fatalf("lifecycle = status %q attempts %d token %v lease %v", status, attempts, token, leaseUntil)
	}
	if (result == nil) != (wantResult == nil) || result != nil && *result != *wantResult {
		t.Fatalf("result = %v, want %v", result, wantResult)
	}
	if (failure == nil) != (wantFailure == nil) || failure != nil && *failure != *wantFailure {
		t.Fatalf("failure_kind = %v, want %v", failure, wantFailure)
	}
	if wantStatus == "completed" && finishedAt == nil {
		t.Fatal("finished_at is NULL for completed submission")
	}
}

func loadSubmissionLease(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) (string, uuid.NullUUID, *time.Time) {
	t.Helper()
	var status string
	var token uuid.NullUUID
	var leaseUntil *time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT status, attempt_token, lease_until FROM submissions WHERE id = $1
	`, id).Scan(&status, &token, &leaseUntil); err != nil {
		t.Fatalf("query submission lease: %v", err)
	}
	return status, token, leaseUntil
}

func loadMatchState(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) (string, uuid.NullUUID) {
	t.Helper()
	var status string
	var winner uuid.NullUUID
	if err := pool.QueryRow(context.Background(), `
		SELECT status, winner_id FROM matches WHERE id = $1
	`, id).Scan(&status, &winner); err != nil {
		t.Fatalf("query match: %v", err)
	}
	return status, winner
}

func reaperIntegrationPostgres(t *testing.T) *pgxpool.Pool {
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
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	return pool
}

func reaperIntegrationRedis(t *testing.T) *redis.Client {
	t.Helper()
	if os.Getenv("CODEDUEL_INTEGRATION") != "1" {
		t.Skip("set CODEDUEL_INTEGRATION=1 to run integration tests")
	}
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	// Isolated from redisx (15), match (14), submission (13), and judge (12).
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 11})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		_ = rdb.Close()
		t.Fatalf("connect to integration Redis: %v", err)
	}
	t.Cleanup(func() {
		_ = rdb.FlushDB(context.Background()).Err()
		_ = rdb.Close()
	})
	return rdb
}

func flushReaperRedis(t *testing.T, rdb *redis.Client) {
	t.Helper()
	if err := rdb.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("FlushDB: %v", err)
	}
}
