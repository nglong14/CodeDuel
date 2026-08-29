package judge

import (
	"context"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nglong14/CodeDuel/internal/infrastructure"
)

func TestPostgresStoreIntegration(t *testing.T) {
	pool := judgeStoreIntegrationPostgres(t)
	store, err := newPostgresStore(pool)
	if err != nil {
		t.Fatalf("newPostgresStore: %v", err)
	}
	ctx := context.Background()

	t.Run("pending claim and live duplicate", func(t *testing.T) {
		fixture := judgeStoreIntegrationFixtureForTest(t, pool, "active")
		token := uuid.New()
		lease := 90 * time.Second
		before := time.Now()

		claim, err := store.Claim(ctx, fixture.submissionID, token, lease)
		if err != nil {
			t.Fatalf("Claim pending submission: %v", err)
		}
		if claim.Kind != claimAcquired {
			t.Fatalf("claim kind = %v, want %v", claim.Kind, claimAcquired)
		}
		if claim.Claimed.SubmissionID != fixture.submissionID || claim.Claimed.MatchID != fixture.matchID ||
			claim.Claimed.PlayerID != fixture.players[0] || claim.Claimed.ProblemID != fixture.problemID {
			t.Fatalf("claimed identifiers = %#v", claim.Claimed)
		}
		if claim.Claimed.Language != LanguagePython || string(claim.Claimed.Source) != judgeStoreIntegrationSource ||
			claim.Claimed.AttemptToken != token {
			t.Fatalf("claimed execution input = %#v", claim.Claimed)
		}
		if claim.Claimed.Players != fixture.players {
			t.Fatalf("claimed players = %v, want slot order %v", claim.Claimed.Players, fixture.players)
		}
		if !reflect.DeepEqual(claim.Claimed.Tests, judgeStoreIntegrationTests()) {
			t.Fatalf("claimed tests = %#v, want %#v", claim.Claimed.Tests, judgeStoreIntegrationTests())
		}

		var status string
		var attempts int
		var storedToken uuid.UUID
		var leaseUntil time.Time
		var result *string
		if err := pool.QueryRow(ctx, `
			SELECT status, attempts, attempt_token, lease_until, result
			FROM submissions
			WHERE id = $1
		`, fixture.submissionID).Scan(&status, &attempts, &storedToken, &leaseUntil, &result); err != nil {
			t.Fatalf("query claimed submission: %v", err)
		}
		if status != "running" || attempts != 1 || storedToken != token || result != nil {
			t.Fatalf("claimed lifecycle = status %q, attempts %d, token %s, result %v", status, attempts, storedToken, result)
		}
		if leaseUntil.Before(before.Add(lease-time.Second)) || leaseUntil.After(time.Now().Add(lease+time.Second)) {
			t.Fatalf("lease_until = %v, want approximately now + %v", leaseUntil, lease)
		}

		duplicate, err := store.Claim(ctx, fixture.submissionID, uuid.New(), lease)
		if err != nil {
			t.Fatalf("Claim live running submission: %v", err)
		}
		if duplicate.Kind != claimRunning || !duplicate.LeaseUntil.Equal(leaseUntil) {
			t.Fatalf("duplicate claim = %#v, want running through %v", duplicate, leaseUntil)
		}
		var duplicateAttempts int
		var duplicateToken uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT attempts, attempt_token FROM submissions WHERE id = $1`, fixture.submissionID).Scan(&duplicateAttempts, &duplicateToken); err != nil {
			t.Fatalf("query duplicate claim state: %v", err)
		}
		if duplicateAttempts != 1 || duplicateToken != token {
			t.Fatalf("duplicate claim mutated attempts/token to %d/%s", duplicateAttempts, duplicateToken)
		}
	})

	t.Run("current token completion and completed claim", func(t *testing.T) {
		fixture := judgeStoreIntegrationFixtureForTest(t, pool, "active")
		claim := judgeStoreIntegrationClaimForTest(t, store, fixture)
		terminal := terminalResult{Verdict: "fail", FailureKind: "wrong_answer", TestsPassed: 1}

		completion, err := store.Complete(ctx, claim, terminal)
		if err != nil {
			t.Fatalf("Complete current claim: %v", err)
		}
		wantCompleted := completedSubmission{
			SubmissionID: fixture.submissionID,
			MatchID:      fixture.matchID,
			PlayerID:     fixture.players[0],
			Players:      fixture.players,
			Verdict:      "fail",
			FailureKind:  "wrong_answer",
			TestsPassed:  1,
			TotalTests:   len(judgeStoreIntegrationTests()),
		}
		if completion.Kind != completionApplied || completion.Completed != wantCompleted {
			t.Fatalf("completion = %#v, want applied %#v", completion, wantCompleted)
		}

		var status, verdict, failureKind string
		var attempts, testsPassed int
		var attemptToken uuid.NullUUID
		var leaseUntil, finishedAt *time.Time
		if err := pool.QueryRow(ctx, `
			SELECT status, result, failure_kind, attempts, tests_passed,
			       attempt_token, lease_until, finished_at
			FROM submissions
			WHERE id = $1
		`, fixture.submissionID).Scan(
			&status, &verdict, &failureKind, &attempts, &testsPassed,
			&attemptToken, &leaseUntil, &finishedAt,
		); err != nil {
			t.Fatalf("query completed submission: %v", err)
		}
		if status != "completed" || verdict != "fail" || failureKind != "wrong_answer" ||
			attempts != 1 || testsPassed != 1 || attemptToken.Valid || leaseUntil != nil || finishedAt == nil {
			t.Fatalf("persisted completion = status %q, verdict %q, failure %q, attempts %d, tests %d, token %v, lease %v, finished %v",
				status, verdict, failureKind, attempts, testsPassed, attemptToken, leaseUntil, finishedAt)
		}

		reconstructed, err := store.Claim(ctx, fixture.submissionID, uuid.New(), time.Minute)
		if err != nil {
			t.Fatalf("Claim completed submission: %v", err)
		}
		if reconstructed.Kind != claimCompleted || reconstructed.Completed != wantCompleted {
			t.Fatalf("completed claim = %#v, want %#v", reconstructed, wantCompleted)
		}
	})

	t.Run("token mismatch loses ownership without mutation", func(t *testing.T) {
		fixture := judgeStoreIntegrationFixtureForTest(t, pool, "active")
		claim := judgeStoreIntegrationClaimForTest(t, store, fixture)
		originalToken := claim.AttemptToken
		var originalLease time.Time
		if err := pool.QueryRow(ctx, `SELECT lease_until FROM submissions WHERE id = $1`, fixture.submissionID).Scan(&originalLease); err != nil {
			t.Fatalf("query original lease: %v", err)
		}
		claim.AttemptToken = uuid.New()

		completion, err := store.Complete(ctx, claim, terminalResult{
			Verdict: "fail", FailureKind: "wrong_answer", TestsPassed: 1,
		})
		if err != nil {
			t.Fatalf("Complete mismatched claim: %v", err)
		}
		if completion.Kind != completionLostOwnership {
			t.Fatalf("completion kind = %v, want %v", completion.Kind, completionLostOwnership)
		}

		var status string
		var attempts, testsPassed int
		var verdict, failureKind *string
		var storedToken uuid.NullUUID
		var leaseUntil, finishedAt *time.Time
		if err := pool.QueryRow(ctx, `
			SELECT status, attempts, result, failure_kind, tests_passed,
			       attempt_token, lease_until, finished_at
			FROM submissions
			WHERE id = $1
		`, fixture.submissionID).Scan(
			&status, &attempts, &verdict, &failureKind, &testsPassed,
			&storedToken, &leaseUntil, &finishedAt,
		); err != nil {
			t.Fatalf("query submission after ownership loss: %v", err)
		}
		if status != "running" || attempts != 1 || verdict != nil || failureKind != nil || testsPassed != 0 ||
			!storedToken.Valid || storedToken.UUID != originalToken || leaseUntil == nil || !leaseUntil.Equal(originalLease) || finishedAt != nil {
			t.Fatalf("submission mutated after ownership loss: status %q, attempts %d, verdict %v, failure %v, tests %d, token %v, lease %v, finished %v",
				status, attempts, verdict, failureKind, testsPassed, storedToken, leaseUntil, finishedAt)
		}
	})

	t.Run("pass finishes active match", func(t *testing.T) {
		fixture := judgeStoreIntegrationFixtureForTest(t, pool, "active")
		claim := judgeStoreIntegrationClaimForTest(t, store, fixture)

		completion, err := store.Complete(ctx, claim, terminalResult{
			Verdict: "pass", TestsPassed: len(claim.Tests),
		})
		if err != nil {
			t.Fatalf("Complete passing claim: %v", err)
		}
		if completion.Kind != completionApplied || completion.Completed.Verdict != "pass" ||
			completion.Completed.WinnerID != fixture.players[0] {
			t.Fatalf("passing completion = %#v", completion)
		}
		var matchStatus string
		var winnerID uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT status, winner_id FROM matches WHERE id = $1`, fixture.matchID).Scan(&matchStatus, &winnerID); err != nil {
			t.Fatalf("query won match: %v", err)
		}
		if matchStatus != "finished" || winnerID != fixture.players[0] {
			t.Fatalf("match after pass = status %q, winner %s", matchStatus, winnerID)
		}
		var submissionStatus, verdict string
		if err := pool.QueryRow(ctx, `SELECT status, result FROM submissions WHERE id = $1`, fixture.submissionID).Scan(&submissionStatus, &verdict); err != nil {
			t.Fatalf("query passing submission: %v", err)
		}
		if submissionStatus != "completed" || verdict != "pass" {
			t.Fatalf("submission after pass = status %q, verdict %q", submissionStatus, verdict)
		}
	})

	t.Run("simultaneous passes choose one winner", func(t *testing.T) {
		fixture := judgeStoreIntegrationFixtureForTest(t, pool, "active")
		secondSubmissionID := uuid.New()
		if _, err := pool.Exec(ctx, `
			INSERT INTO submissions (id, match_id, player_id, request_id, language, code)
			VALUES ($1, $2, $3, $4, 'python', $5)
		`, secondSubmissionID, fixture.matchID, fixture.players[1], uuid.New(), judgeStoreIntegrationSource); err != nil {
			t.Fatalf("insert second player submission: %v", err)
		}

		firstClaim := judgeStoreIntegrationClaimForTest(t, store, fixture)
		secondFixture := fixture
		secondFixture.submissionID = secondSubmissionID
		secondClaim := judgeStoreIntegrationClaimForTest(t, store, secondFixture)

		type completionCall struct {
			completion completionResult
			err        error
		}
		start := make(chan struct{})
		var startOnce sync.Once
		releaseStart := func() { startOnce.Do(func() { close(start) }) }
		defer releaseStart()
		results := make(chan completionCall, 2)
		var ready sync.WaitGroup
		ready.Add(2)
		for _, claim := range []claimedSubmission{firstClaim, secondClaim} {
			go func() {
				ready.Done()
				<-start
				completion, err := store.Complete(ctx, claim, terminalResult{
					Verdict: "pass", TestsPassed: len(claim.Tests),
				})
				results <- completionCall{completion: completion, err: err}
			}()
		}
		allReady := make(chan struct{})
		go func() {
			ready.Wait()
			close(allReady)
		}()
		receiveJudgeTest(t, allReady, "simultaneous completion readiness")
		releaseStart()

		calls := [2]completionCall{
			receiveJudgeTest(t, results, "first simultaneous completion"),
			receiveJudgeTest(t, results, "second simultaneous completion"),
		}
		for i, call := range calls {
			if call.err != nil {
				t.Fatalf("concurrent Complete call %d: %v", i+1, call.err)
			}
			if call.completion.Kind != completionApplied {
				t.Fatalf("concurrent completion %d kind = %v, want %v", i+1, call.completion.Kind, completionApplied)
			}
		}
		winnerID := calls[0].completion.Completed.WinnerID
		if winnerID == uuid.Nil || calls[1].completion.Completed.WinnerID != winnerID {
			t.Fatalf("concurrent completion winners = %s and %s, want same non-nil winner",
				winnerID, calls[1].completion.Completed.WinnerID)
		}
		if winnerID != fixture.players[0] && winnerID != fixture.players[1] {
			t.Fatalf("concurrent completion winner = %s, want one of %v", winnerID, fixture.players)
		}

		var matchStatus string
		var storedWinner uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT status, winner_id FROM matches WHERE id = $1`, fixture.matchID).Scan(&matchStatus, &storedWinner); err != nil {
			t.Fatalf("query concurrently won match: %v", err)
		}
		if matchStatus != "finished" || storedWinner != winnerID {
			t.Fatalf("match after concurrent passes = status %q, winner %s; want finished/%s", matchStatus, storedWinner, winnerID)
		}
		for _, submissionID := range []uuid.UUID{fixture.submissionID, secondSubmissionID} {
			var submissionStatus, verdict string
			if err := pool.QueryRow(ctx, `SELECT status, result FROM submissions WHERE id = $1`, submissionID).Scan(&submissionStatus, &verdict); err != nil {
				t.Fatalf("query concurrent passing submission %s: %v", submissionID, err)
			}
			if submissionStatus != "completed" || verdict != "pass" {
				t.Fatalf("submission %s after concurrent pass = status %q, verdict %q", submissionID, submissionStatus, verdict)
			}
		}
	})

	t.Run("pass preserves finished match winner", func(t *testing.T) {
		fixture := judgeStoreIntegrationFixtureForTest(t, pool, "finished")
		existingWinner := fixture.players[1]
		if _, err := pool.Exec(ctx, `UPDATE matches SET winner_id = $1 WHERE id = $2`, existingWinner, fixture.matchID); err != nil {
			t.Fatalf("set existing winner: %v", err)
		}
		claim := judgeStoreIntegrationClaimForTest(t, store, fixture)

		completion, err := store.Complete(ctx, claim, terminalResult{
			Verdict: "pass", TestsPassed: len(claim.Tests),
		})
		if err != nil {
			t.Fatalf("Complete pass for finished match: %v", err)
		}
		if completion.Kind != completionApplied || completion.Completed.Verdict != "pass" ||
			completion.Completed.WinnerID != existingWinner {
			t.Fatalf("finished-match completion = %#v", completion)
		}
		var matchStatus, verdict string
		var winnerID uuid.UUID
		if err := pool.QueryRow(ctx, `
			SELECT m.status, m.winner_id, s.result
			FROM matches m
			JOIN submissions s ON s.match_id = m.id
			WHERE m.id = $1 AND s.id = $2
		`, fixture.matchID, fixture.submissionID).Scan(&matchStatus, &winnerID, &verdict); err != nil {
			t.Fatalf("query finished match and submission: %v", err)
		}
		if matchStatus != "finished" || winnerID != existingWinner || verdict != "pass" {
			t.Fatalf("persisted finished-match pass = status %q, winner %s, verdict %q", matchStatus, winnerID, verdict)
		}
	})
}

const judgeStoreIntegrationSource = "print('candidate')\n"

type judgeStoreIntegrationFixture struct {
	matchID      uuid.UUID
	problemID    uuid.UUID
	submissionID uuid.UUID
	players      [2]uuid.UUID
}

func judgeStoreIntegrationFixtureForTest(
	t *testing.T,
	pool *pgxpool.Pool,
	matchStatus string,
) judgeStoreIntegrationFixture {
	t.Helper()
	ctx := context.Background()
	fixture := judgeStoreIntegrationFixture{players: [2]uuid.UUID{uuid.New(), uuid.New()}}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id) VALUES ($1), ($2)`, fixture.players[0], fixture.players[1]); err != nil {
		t.Fatalf("insert fixture users: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO problems (title, statement, test_cases)
		VALUES ('Judge store integration problem', 'Test statement', $1::jsonb)
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
		VALUES ($1, $2, now() + interval '1 hour')
		RETURNING id
	`, fixture.problemID, matchStatus).Scan(&fixture.matchID); err != nil {
		t.Fatalf("insert fixture match: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO match_players (match_id, user_id, slot)
		VALUES ($1, $2, 2), ($1, $3, 1)
	`, fixture.matchID, fixture.players[1], fixture.players[0]); err != nil {
		t.Fatalf("insert fixture match players: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO submissions (match_id, player_id, request_id, language, code)
		VALUES ($1, $2, $3, 'python', $4)
		RETURNING id
	`, fixture.matchID, fixture.players[0], uuid.New(), judgeStoreIntegrationSource).Scan(&fixture.submissionID); err != nil {
		t.Fatalf("insert fixture submission: %v", err)
	}
	return fixture
}

func judgeStoreIntegrationClaimForTest(
	t *testing.T,
	store *postgresStore,
	fixture judgeStoreIntegrationFixture,
) claimedSubmission {
	t.Helper()
	claim, err := store.Claim(context.Background(), fixture.submissionID, uuid.New(), time.Minute)
	if err != nil {
		t.Fatalf("Claim fixture submission: %v", err)
	}
	if claim.Kind != claimAcquired {
		t.Fatalf("fixture claim kind = %v, want %v", claim.Kind, claimAcquired)
	}
	return claim.Claimed
}

func judgeStoreIntegrationTests() []TestCase {
	return []TestCase{
		{Input: []byte("first input\n"), Expected: []byte("first output\n")},
		{Input: []byte("second input\n"), Expected: []byte("second output\n")},
		{Input: []byte("third input\n"), Expected: []byte("third output\n")},
	}
}

func judgeStoreIntegrationPostgres(t *testing.T) *pgxpool.Pool {
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
