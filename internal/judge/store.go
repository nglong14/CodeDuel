package judge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type claimKind uint8

const (
	claimAcquired claimKind = iota
	claimCompleted
	claimRunning
	claimExpired
	claimMissing
)

type claimedSubmission struct {
	SubmissionID uuid.UUID
	MatchID      uuid.UUID
	PlayerID     uuid.UUID
	ProblemID    uuid.UUID
	Language     Language
	Source       []byte
	Tests        []TestCase
	Players      [2]uuid.UUID
	AttemptToken uuid.UUID
}

type completedSubmission struct {
	SubmissionID uuid.UUID
	MatchID      uuid.UUID
	PlayerID     uuid.UUID
	Players      [2]uuid.UUID
	Verdict      string
	FailureKind  string
	TestsPassed  int
	TotalTests   int
	WinnerID     uuid.UUID
}

type claimResult struct {
	Kind       claimKind
	Claimed    claimedSubmission
	Completed  completedSubmission
	LeaseUntil time.Time
}

type terminalResult struct {
	Verdict     string
	FailureKind string
	TestsPassed int
}

type completionKind uint8

const (
	completionApplied completionKind = iota
	completionLostOwnership
)

type completionResult struct {
	Kind      completionKind
	Completed completedSubmission
}

type submissionStore interface {
	Claim(context.Context, uuid.UUID, uuid.UUID, time.Duration) (claimResult, error)
	Complete(context.Context, claimedSubmission, terminalResult) (completionResult, error)
}

type postgresStore struct {
	db *pgxpool.Pool
}

func newPostgresStore(db *pgxpool.Pool) (*postgresStore, error) {
	if db == nil {
		return nil, errors.New("initialize judge store: missing PostgreSQL pool")
	}
	return &postgresStore{db: db}, nil
}

func (s *postgresStore) Claim(
	ctx context.Context,
	submissionID uuid.UUID,
	attemptToken uuid.UUID,
	lease time.Duration,
) (claimResult, error) {
	if s == nil || s.db == nil || submissionID == uuid.Nil || attemptToken == uuid.Nil || lease <= 0 {
		return claimResult{}, errors.New("claim submission: invalid arguments")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return claimResult{}, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		status        string
		matchID       uuid.UUID
		playerID      uuid.UUID
		problemID     uuid.UUID
		language      string
		code          string
		testCasesJSON []byte
		storedToken   uuid.NullUUID
		leaseUntil    *time.Time
		verdict       *string
		failureKind   *string
		testsPassed   int
		winnerID      uuid.NullUUID
		now           time.Time
	)
	err = tx.QueryRow(ctx, `
		SELECT s.status, s.match_id, s.player_id, m.problem_id, s.language, s.code,
		       p.test_cases, s.attempt_token, s.lease_until, s.result,
		       s.failure_kind, s.tests_passed, m.winner_id, clock_timestamp()
		FROM submissions s
		JOIN matches m ON m.id = s.match_id
		JOIN problems p ON p.id = m.problem_id
		WHERE s.id = $1
		FOR UPDATE OF s
	`, submissionID).Scan(
		&status,
		&matchID,
		&playerID,
		&problemID,
		&language,
		&code,
		&testCasesJSON,
		&storedToken,
		&leaseUntil,
		&verdict,
		&failureKind,
		&testsPassed,
		&winnerID,
		&now,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return claimResult{Kind: claimMissing}, nil
	}
	if err != nil {
		return claimResult{}, fmt.Errorf("lock submission: %w", err)
	}

	switch status {
	case "running":
		if err := tx.Commit(ctx); err != nil {
			return claimResult{}, fmt.Errorf("commit running claim inspection: %w", err)
		}
		if leaseUntil != nil && leaseUntil.After(now) && storedToken.Valid {
			return claimResult{Kind: claimRunning, LeaseUntil: *leaseUntil}, nil
		}
		return claimResult{Kind: claimExpired}, nil
	case "completed":
		if verdict == nil {
			return claimResult{}, errors.New("load completed submission: missing verdict")
		}
		tests, err := decodeStoredTests(testCasesJSON)
		if err != nil {
			return claimResult{}, fmt.Errorf("load completed submission tests: %w", err)
		}
		players, err := loadMatchPlayers(ctx, tx, matchID, playerID)
		if err != nil {
			return claimResult{}, err
		}
		completed := completedSubmission{
			SubmissionID: submissionID,
			MatchID:      matchID,
			PlayerID:     playerID,
			Players:      players,
			Verdict:      *verdict,
			TestsPassed:  testsPassed,
			TotalTests:   len(tests),
		}
		if failureKind != nil {
			completed.FailureKind = *failureKind
		}
		if winnerID.Valid {
			completed.WinnerID = winnerID.UUID
		}
		if err := tx.Commit(ctx); err != nil {
			return claimResult{}, fmt.Errorf("commit completed claim inspection: %w", err)
		}
		return claimResult{Kind: claimCompleted, Completed: completed}, nil
	case "pending":
	default:
		return claimResult{}, fmt.Errorf("claim submission: invalid status %q", status)
	}

	tests, err := decodeStoredTests(testCasesJSON)
	if err != nil {
		return claimResult{}, fmt.Errorf("load submission tests: %w", err)
	}
	parsedLanguage, err := parseStoredLanguage(language)
	if err != nil {
		return claimResult{}, err
	}
	if code == "" || !utf8.ValidString(code) {
		return claimResult{}, errors.New("load submission: invalid source")
	}
	players, err := loadMatchPlayers(ctx, tx, matchID, playerID)
	if err != nil {
		return claimResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE submissions
		SET status = 'running',
		    attempts = attempts + 1,
		    attempt_token = $2,
		    lease_until = clock_timestamp() + ($3 * interval '1 microsecond')
		WHERE id = $1 AND status = 'pending'
	`, submissionID, attemptToken, lease.Microseconds()); err != nil {
		return claimResult{}, fmt.Errorf("mark submission running: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return claimResult{}, fmt.Errorf("commit submission claim: %w", err)
	}
	return claimResult{
		Kind: claimAcquired,
		Claimed: claimedSubmission{
			SubmissionID: submissionID,
			MatchID:      matchID,
			PlayerID:     playerID,
			ProblemID:    problemID,
			Language:     parsedLanguage,
			Source:       []byte(code),
			Tests:        tests,
			Players:      players,
			AttemptToken: attemptToken,
		},
	}, nil
}

func (s *postgresStore) Complete(
	ctx context.Context,
	claimed claimedSubmission,
	result terminalResult,
) (completionResult, error) {
	if s == nil || s.db == nil || !validClaimedSubmission(claimed) {
		return completionResult{}, errors.New("complete submission: invalid claim")
	}
	if err := validateTerminalResult(result, len(claimed.Tests)); err != nil {
		return completionResult{}, fmt.Errorf("complete submission: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return completionResult{}, fmt.Errorf("begin completion transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var matchStatus string
	var winnerID uuid.NullUUID
	if err := tx.QueryRow(ctx, `
		SELECT status, winner_id
		FROM matches
		WHERE id = $1
		FOR UPDATE
	`, claimed.MatchID).Scan(&matchStatus, &winnerID); err != nil {
		return completionResult{}, fmt.Errorf("lock completion match: %w", err)
	}

	var status string
	var matchID, playerID uuid.UUID
	var currentToken uuid.NullUUID
	if err := tx.QueryRow(ctx, `
		SELECT status, match_id, player_id, attempt_token
		FROM submissions
		WHERE id = $1
		FOR UPDATE
	`, claimed.SubmissionID).Scan(&status, &matchID, &playerID, &currentToken); err != nil {
		return completionResult{}, fmt.Errorf("lock completion submission: %w", err)
	}
	if status != "running" || matchID != claimed.MatchID || playerID != claimed.PlayerID ||
		!currentToken.Valid || currentToken.UUID != claimed.AttemptToken {
		if err := tx.Commit(ctx); err != nil {
			return completionResult{}, fmt.Errorf("commit lost completion ownership: %w", err)
		}
		return completionResult{Kind: completionLostOwnership}, nil
	}

	var nullableFailure any
	if result.FailureKind != "" {
		nullableFailure = result.FailureKind
	}
	if _, err := tx.Exec(ctx, `
		UPDATE submissions
		SET status = 'completed',
		    result = $2,
		    failure_kind = $3,
		    tests_passed = $4,
		    finished_at = clock_timestamp(),
		    attempt_token = NULL,
		    lease_until = NULL
		WHERE id = $1
	`, claimed.SubmissionID, result.Verdict, nullableFailure, result.TestsPassed); err != nil {
		return completionResult{}, fmt.Errorf("persist submission result: %w", err)
	}

	if result.Verdict == "pass" && matchStatus == "active" && !winnerID.Valid {
		var selected uuid.UUID
		err := tx.QueryRow(ctx, `
			UPDATE matches
			SET status = 'finished', winner_id = $1
			WHERE id = $2 AND status = 'active' AND winner_id IS NULL
			RETURNING winner_id
		`, claimed.PlayerID, claimed.MatchID).Scan(&selected)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return completionResult{}, fmt.Errorf("claim match winner: %w", err)
		}
		if err == nil {
			winnerID = uuid.NullUUID{UUID: selected, Valid: true}
		} else if err := tx.QueryRow(ctx, `SELECT winner_id FROM matches WHERE id = $1`, claimed.MatchID).Scan(&winnerID); err != nil {
			return completionResult{}, fmt.Errorf("load established winner: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return completionResult{}, fmt.Errorf("commit submission result: %w", err)
	}
	completed := completedSubmission{
		SubmissionID: claimed.SubmissionID,
		MatchID:      claimed.MatchID,
		PlayerID:     claimed.PlayerID,
		Players:      claimed.Players,
		Verdict:      result.Verdict,
		FailureKind:  result.FailureKind,
		TestsPassed:  result.TestsPassed,
		TotalTests:   len(claimed.Tests),
	}
	if winnerID.Valid {
		completed.WinnerID = winnerID.UUID
	}
	return completionResult{Kind: completionApplied, Completed: completed}, nil
}

type storedTestCase struct {
	Input    *string `json:"input"`
	Expected *string `json:"expected"`
}

func decodeStoredTests(raw []byte) ([]TestCase, error) {
	var stored []storedTestCase
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("decode test cases: %w", err)
	}
	if len(stored) == 0 {
		return nil, errors.New("decode test cases: no test cases")
	}
	tests := make([]TestCase, len(stored))
	for index, test := range stored {
		if test.Input == nil || test.Expected == nil || !utf8.ValidString(*test.Input) || !utf8.ValidString(*test.Expected) {
			return nil, fmt.Errorf("decode test case %d: invalid input or expected output", index+1)
		}
		tests[index] = TestCase{Input: []byte(*test.Input), Expected: []byte(*test.Expected)}
	}
	return tests, nil
}

func parseStoredLanguage(value string) (Language, error) {
	language := Language(value)
	switch language {
	case LanguagePython, LanguageCPP, LanguageJava:
		return language, nil
	default:
		return "", fmt.Errorf("load submission: unsupported language %q", value)
	}
}

func loadMatchPlayers(
	ctx context.Context,
	tx pgx.Tx,
	matchID, submittingPlayer uuid.UUID,
) ([2]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT user_id
		FROM match_players
		WHERE match_id = $1
		ORDER BY slot, user_id
	`, matchID)
	if err != nil {
		return [2]uuid.UUID{}, fmt.Errorf("load match players: %w", err)
	}
	defer rows.Close()
	var players [2]uuid.UUID
	count := 0
	containsSubmitter := false
	for rows.Next() {
		if count >= len(players) {
			return [2]uuid.UUID{}, errors.New("load match players: expected exactly two players")
		}
		if err := rows.Scan(&players[count]); err != nil {
			return [2]uuid.UUID{}, fmt.Errorf("scan match player: %w", err)
		}
		containsSubmitter = containsSubmitter || players[count] == submittingPlayer
		count++
	}
	if err := rows.Err(); err != nil {
		return [2]uuid.UUID{}, fmt.Errorf("iterate match players: %w", err)
	}
	if count != len(players) || !containsSubmitter || players[0] == players[1] {
		return [2]uuid.UUID{}, errors.New("load match players: invalid player set")
	}
	return players, nil
}

func validClaimedSubmission(claimed claimedSubmission) bool {
	return claimed.SubmissionID != uuid.Nil && claimed.MatchID != uuid.Nil && claimed.PlayerID != uuid.Nil &&
		claimed.AttemptToken != uuid.Nil && len(claimed.Tests) > 0
}

func validateTerminalResult(result terminalResult, totalTests int) error {
	if result.TestsPassed < 0 || result.TestsPassed > totalTests {
		return errors.New("invalid tests passed")
	}
	switch result.Verdict {
	case "pass":
		if result.FailureKind != "" || result.TestsPassed != totalTests {
			return errors.New("invalid pass result")
		}
	case "fail":
		if result.FailureKind != "wrong_answer" {
			return errors.New("invalid fail result")
		}
	case "error":
		switch result.FailureKind {
		case "compile_error", "runtime_error", "output_limit":
		default:
			return errors.New("invalid error result")
		}
	case "timeout":
		if result.FailureKind != "" {
			return errors.New("invalid timeout result")
		}
	default:
		return fmt.Errorf("invalid verdict %q", result.Verdict)
	}
	return nil
}
