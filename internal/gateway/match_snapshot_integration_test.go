package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nglong14/CodeDuel/internal/app"
	"github.com/nglong14/CodeDuel/internal/config"
)

const (
	hiddenInputCanary    = "HIDDEN_INPUT_CANARY"
	hiddenExpectedCanary = "HIDDEN_EXPECTED_CANARY"
	callerSourceCanary   = "CALLER_SOURCE_CANARY"
	opponentSourceCanary = "OPPONENT_SOURCE_CANARY"
)

type matchSnapshotFixture struct {
	matchID uuid.UUID
	players [2]uuid.UUID
}

func TestCurrentMatchSnapshotIntegration(t *testing.T) {
	pool := gatewayIntegrationPostgres(t)
	handler := gatewayIntegrationHandler(pool)
	ctx := context.Background()
	players := insertSnapshotUsers(t, pool)
	problemID := insertSnapshotProblem(t, pool)
	active := insertSnapshotMatch(t, pool, problemID, players, "active", time.Now().Add(-2*time.Hour), uuid.Nil)
	_ = insertSnapshotMatch(t, pool, problemID, players, "finished", time.Now().Add(-time.Hour), players[1])

	createdAt := time.Now().Add(-30 * time.Minute).UTC().Truncate(time.Microsecond)
	firstSubmissionID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	secondSubmissionID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	insertSnapshotSubmission(t, pool, active.matchID, players[0], firstSubmissionID, "completed", 2, callerSourceCanary, createdAt)
	insertSnapshotSubmission(t, pool, active.matchID, players[0], secondSubmissionID, "pending", 0, callerSourceCanary+"_PENDING", createdAt)
	insertSnapshotSubmission(t, pool, active.matchID, players[1], uuid.New(), "completed", 3, opponentSourceCanary, createdAt)

	recorder := serveAuthenticatedMatch(t, handler, "/api/me/match", players[0])
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var response matchResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode match response: %v", err)
	}
	if response.Match == nil || response.Match.ID != active.matchID || response.Match.Status != "active" {
		t.Fatalf("current match = %#v, want active %s", response.Match, active.matchID)
	}
	if response.Match.WinnerID != nil || response.Match.Outcome != nil {
		t.Fatalf("active outcome = winner %v outcome %v", response.Match.WinnerID, response.Match.Outcome)
	}
	if response.Match.Truncated {
		t.Fatal("two submissions were reported as truncated")
	}
	if response.Match.ServerTime.IsZero() || response.Match.Problem.ID != problemID || response.Match.Problem.TotalTests != 3 {
		t.Fatalf("snapshot metadata = %#v", response.Match)
	}
	if len(response.Match.Players) != 2 || response.Match.Players[0].ID != players[0] ||
		response.Match.Players[0].BestTestsPassed != 2 || response.Match.Players[1].ID != players[1] ||
		response.Match.Players[1].BestTestsPassed != 3 {
		t.Fatalf("players = %#v", response.Match.Players)
	}
	if len(response.Match.Submissions) != 2 || response.Match.Submissions[0].ID != firstSubmissionID ||
		response.Match.Submissions[1].ID != secondSubmissionID || response.Match.Submissions[0].Verdict == nil ||
		*response.Match.Submissions[0].Verdict != "fail" || response.Match.Submissions[1].Verdict != nil {
		t.Fatalf("submissions = %#v", response.Match.Submissions)
	}
	for _, secret := range []string{
		hiddenInputCanary,
		hiddenExpectedCanary,
		callerSourceCanary,
		opponentSourceCanary,
		players[0].String() + "@snapshot.test",
		players[1].String() + "@snapshot.test",
	} {
		if strings.Contains(recorder.Body.String(), secret) {
			t.Fatalf("match response leaked %q: %s", secret, recorder.Body.String())
		}
	}

	var claimCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM active_match_players WHERE match_id = $1`, active.matchID).Scan(&claimCount); err != nil {
		t.Fatalf("count active claims: %v", err)
	}
	if claimCount != 2 {
		t.Fatalf("active claims = %d, want 2", claimCount)
	}
}

func TestMatchSnapshotBoundsSubmissionHistoryIntegration(t *testing.T) {
	pool := gatewayIntegrationPostgres(t)
	handler := gatewayIntegrationHandler(pool)
	players := insertSnapshotUsers(t, pool)
	problemID := insertSnapshotProblem(t, pool)
	match := insertSnapshotMatch(t, pool, problemID, players, "active", time.Now().Add(-time.Hour), uuid.Nil)
	createdAt := time.Now().Add(-30 * time.Minute).UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO submissions (match_id, player_id, request_id, language, code, created_at)
		SELECT $1, $2, gen_random_uuid(), 'python', 'bounded source', $3::timestamptz + (n * interval '1 second')
		FROM generate_series(0, $4 - 1) AS n
	`, match.matchID, players[0], createdAt, maxSnapshotSubmissions+1); err != nil {
		t.Fatalf("insert bounded submissions: %v", err)
	}

	recorder := serveAuthenticatedMatch(t, handler, "/api/me/match", players[0])
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response matchResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Match == nil || !response.Match.Truncated || len(response.Match.Submissions) != maxSnapshotSubmissions {
		t.Fatalf("bounded snapshot = %#v", response.Match)
	}
	if !response.Match.Submissions[0].CreatedAt.Equal(createdAt.Add(time.Second)) ||
		!response.Match.Submissions[maxSnapshotSubmissions-1].CreatedAt.Equal(createdAt.Add(maxSnapshotSubmissions*time.Second)) {
		t.Fatalf("bounded submission range = %v through %v", response.Match.Submissions[0].CreatedAt, response.Match.Submissions[maxSnapshotSubmissions-1].CreatedAt)
	}
}

func TestFinishedMatchSnapshotAndAuthorizationIntegration(t *testing.T) {
	pool := gatewayIntegrationPostgres(t)
	handler := gatewayIntegrationHandler(pool)
	players := insertSnapshotUsers(t, pool)
	outsider := insertSnapshotUsers(t, pool)[0]
	problemID := insertSnapshotProblem(t, pool)
	won := insertSnapshotMatch(t, pool, problemID, players, "finished", time.Now().Add(-2*time.Hour), players[0])
	draw := insertSnapshotMatch(t, pool, problemID, players, "finished", time.Now().Add(-time.Hour), uuid.Nil)

	for _, test := range []struct {
		name        string
		path        string
		userID      uuid.UUID
		wantStatus  int
		wantMatchID uuid.UUID
		wantOutcome string
	}{
		{"winner by ID", "/api/matches/" + won.matchID.String(), players[0], http.StatusOK, won.matchID, "win"},
		{"loser by ID", "/api/matches/" + won.matchID.String(), players[1], http.StatusOK, won.matchID, "loss"},
		{"latest finished", "/api/me/match", players[0], http.StatusOK, draw.matchID, "draw"},
		{"outsider", "/api/matches/" + won.matchID.String(), outsider, http.StatusNotFound, uuid.Nil, ""},
		{"missing match", "/api/matches/" + uuid.NewString(), players[0], http.StatusNotFound, uuid.Nil, ""},
		{"malformed match ID", "/api/matches/not-a-uuid", players[0], http.StatusNotFound, uuid.Nil, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := serveAuthenticatedMatch(t, handler, test.path, test.userID)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if test.wantStatus != http.StatusOK {
				var response errorResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if response.Error.Code != "match_not_found" {
					t.Fatalf("error code = %q, want match_not_found", response.Error.Code)
				}
				return
			}
			var response matchResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Match == nil || response.Match.ID != test.wantMatchID || response.Match.Outcome == nil || *response.Match.Outcome != test.wantOutcome {
				t.Fatalf("match = %#v, want %s outcome %q", response.Match, test.wantMatchID, test.wantOutcome)
			}
		})
	}
}

func TestCurrentMatchSnapshotReturnsNullIntegration(t *testing.T) {
	pool := gatewayIntegrationPostgres(t)
	handler := gatewayIntegrationHandler(pool)
	userID := insertSnapshotUsers(t, pool)[0]
	recorder := serveAuthenticatedMatch(t, handler, "/api/me/match", userID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response matchResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Match != nil || !strings.Contains(recorder.Body.String(), `"match":null`) {
		t.Fatalf("response = %s, want null match", recorder.Body.String())
	}
}

func TestMatchSnapshotRequiresBearerAuthenticationIntegration(t *testing.T) {
	pool := gatewayIntegrationPostgres(t)
	handler := gatewayIntegrationHandler(pool)
	for _, path := range []string{"/api/me/match", "/api/matches/not-a-uuid"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401", path, recorder.Code)
		}
		if got := recorder.Header().Get("WWW-Authenticate"); got != "Bearer" {
			t.Fatalf("%s WWW-Authenticate = %q, want Bearer", path, got)
		}
	}
}

func gatewayIntegrationHandler(pool *pgxpool.Pool) http.Handler {
	deps := &app.Dependencies{
		Config:   &config.Config{Gateway: config.GatewayConfig{JWTSecret: testSecret}},
		Logger:   slog.New(slog.DiscardHandler),
		Postgres: pool,
	}
	return newHandler(context.Background(), deps, NewRegistry())
}

func serveAuthenticatedMatch(t *testing.T, handler http.Handler, path string, userID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	token, err := MintToken(userID, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func insertSnapshotUsers(t *testing.T, pool *pgxpool.Pool) [2]uuid.UUID {
	t.Helper()
	players := [2]uuid.UUID{uuid.New(), uuid.New()}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, email, display_name)
		VALUES ($1, $3::text || '@snapshot.test', 'snapshot-one'),
		       ($2, $4::text || '@snapshot.test', 'snapshot-two')
	`, players[0], players[1], players[0].String(), players[1].String()); err != nil {
		t.Fatalf("insert snapshot users: %v", err)
	}
	return players
}

func insertSnapshotProblem(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var problemID uuid.UUID
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO problems (title, statement, test_cases)
		VALUES ('Snapshot problem', 'Public problem statement', $1::jsonb)
		RETURNING id
	`, `[{"input":"`+hiddenInputCanary+`","expected":"`+hiddenExpectedCanary+`"},{"input":"2","expected":"2"},{"input":"3","expected":"3"}]`).Scan(&problemID); err != nil {
		t.Fatalf("insert snapshot problem: %v", err)
	}
	return problemID
}

func insertSnapshotMatch(
	t *testing.T,
	pool *pgxpool.Pool,
	problemID uuid.UUID,
	players [2]uuid.UUID,
	status string,
	createdAt time.Time,
	winnerID uuid.UUID,
) matchSnapshotFixture {
	t.Helper()
	ctx := context.Background()
	var matchID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO matches (problem_id, status, deadline, created_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, problemID, status, createdAt.Add(time.Hour), createdAt).Scan(&matchID); err != nil {
		t.Fatalf("insert snapshot match: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO match_players (match_id, user_id, slot)
		VALUES ($1, $2, 1), ($1, $3, 2)
	`, matchID, players[0], players[1]); err != nil {
		t.Fatalf("insert snapshot players: %v", err)
	}
	if winnerID != uuid.Nil {
		if _, err := pool.Exec(ctx, `UPDATE matches SET winner_id = $2 WHERE id = $1`, matchID, winnerID); err != nil {
			t.Fatalf("set snapshot winner: %v", err)
		}
	}
	return matchSnapshotFixture{matchID: matchID, players: players}
}

func insertSnapshotSubmission(
	t *testing.T,
	pool *pgxpool.Pool,
	matchID, playerID, submissionID uuid.UUID,
	status string,
	testsPassed int,
	code string,
	createdAt time.Time,
) {
	t.Helper()
	ctx := context.Background()
	if status == "completed" {
		if _, err := pool.Exec(ctx, `
			INSERT INTO submissions (
				id, match_id, player_id, request_id, language, code,
				status, result, failure_kind, tests_passed, created_at, finished_at
			)
			VALUES ($1, $2, $3, $4, 'python', $5, 'completed', 'fail', 'wrong_answer', $6, $7, $7)
		`, submissionID, matchID, playerID, uuid.New(), code, testsPassed, createdAt); err != nil {
			t.Fatalf("insert completed snapshot submission: %v", err)
		}
		return
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO submissions (id, match_id, player_id, request_id, language, code, created_at)
		VALUES ($1, $2, $3, $4, 'python', $5, $6)
	`, submissionID, matchID, playerID, uuid.New(), code, createdAt); err != nil {
		t.Fatalf("insert pending snapshot submission: %v", err)
	}
}
