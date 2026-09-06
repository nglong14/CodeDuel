package gateway

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxSnapshotSubmissions = 100

var errMatchNotFound = errors.New("match not found")

type problemSnapshot struct {
	ID         uuid.UUID `json:"id"`
	Title      string    `json:"title"`
	Statement  string    `json:"statement"`
	TotalTests int       `json:"total_tests"`
}

type playerSnapshot struct {
	ID              uuid.UUID `json:"id"`
	DisplayName     string    `json:"display_name"`
	Slot            int       `json:"slot"`
	BestTestsPassed int       `json:"best_tests_passed"`
}

type submissionSnapshot struct {
	ID          uuid.UUID  `json:"id"`
	RequestID   uuid.UUID  `json:"request_id"`
	Language    string     `json:"language"`
	Status      string     `json:"status"`
	Verdict     *string    `json:"verdict"`
	FailureKind *string    `json:"failure_kind"`
	TestsPassed int        `json:"tests_passed"`
	CreatedAt   time.Time  `json:"created_at"`
	FinishedAt  *time.Time `json:"finished_at"`
}

type matchSnapshot struct {
	ID          uuid.UUID            `json:"id"`
	Status      string               `json:"status"`
	CreatedAt   time.Time            `json:"created_at"`
	Deadline    time.Time            `json:"deadline"`
	WinnerID    *uuid.UUID           `json:"winner_id"`
	Outcome     *string              `json:"outcome"`
	ServerTime  time.Time            `json:"server_time"`
	Problem     problemSnapshot      `json:"problem"`
	Players     []playerSnapshot     `json:"players"`
	Submissions []submissionSnapshot `json:"submissions"`
	Truncated   bool                 `json:"submissions_truncated"`
}

type matchSnapshotService struct {
	db *pgxpool.Pool
}

func newMatchSnapshotService(db *pgxpool.Pool) *matchSnapshotService {
	return &matchSnapshotService{db: db}
}

func (s *matchSnapshotService) current(ctx context.Context, userID uuid.UUID) (*matchSnapshot, error) {
	return s.load(ctx, userID, `
		SELECT m.id, m.status, m.created_at, m.deadline, m.winner_id,
		       p.id, p.title, p.statement, jsonb_array_length(p.test_cases),
		       clock_timestamp()
		FROM matches m
		JOIN match_players self ON self.match_id = m.id AND self.user_id = $1
		JOIN problems p ON p.id = m.problem_id
		ORDER BY (m.status = 'active') DESC, m.created_at DESC, m.id DESC
		LIMIT 1
	`, userID)
}

func (s *matchSnapshotService) byID(ctx context.Context, userID, matchID uuid.UUID) (*matchSnapshot, error) {
	return s.load(ctx, userID, `
		SELECT m.id, m.status, m.created_at, m.deadline, m.winner_id,
		       p.id, p.title, p.statement, jsonb_array_length(p.test_cases),
		       clock_timestamp()
		FROM matches m
		JOIN match_players self ON self.match_id = m.id AND self.user_id = $1
		JOIN problems p ON p.id = m.problem_id
		WHERE m.id = $2
	`, userID, matchID)
}

func (s *matchSnapshotService) load(
	ctx context.Context,
	userID uuid.UUID,
	query string,
	args ...any,
) (*matchSnapshot, error) {
	if s == nil || s.db == nil || userID == uuid.Nil {
		return nil, errors.New("load match snapshot: invalid arguments")
	}
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("load match snapshot: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	snapshot := &matchSnapshot{
		Players:     make([]playerSnapshot, 0, 2),
		Submissions: make([]submissionSnapshot, 0),
	}
	var winnerID uuid.NullUUID
	err = tx.QueryRow(ctx, query, args...).Scan(
		&snapshot.ID,
		&snapshot.Status,
		&snapshot.CreatedAt,
		&snapshot.Deadline,
		&winnerID,
		&snapshot.Problem.ID,
		&snapshot.Problem.Title,
		&snapshot.Problem.Statement,
		&snapshot.Problem.TotalTests,
		&snapshot.ServerTime,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("load match snapshot: commit empty result: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load match snapshot: query match: %w", err)
	}
	if winnerID.Valid {
		winner := winnerID.UUID
		snapshot.WinnerID = &winner
	}
	if err := setSnapshotOutcome(snapshot, userID); err != nil {
		return nil, err
	}

	players, err := tx.Query(ctx, `
		SELECT mp.user_id, u.display_name, mp.slot,
		       COALESCE(best.tests_passed, 0)
		FROM match_players mp
		JOIN users u ON u.id = mp.user_id
		LEFT JOIN LATERAL (
			SELECT tests_passed
			FROM submissions
			WHERE match_id = mp.match_id
			  AND player_id = mp.user_id
			  AND status = 'completed'
			ORDER BY tests_passed DESC
			LIMIT 1
		) best ON true
		WHERE mp.match_id = $1
		ORDER BY mp.slot, mp.user_id
	`, snapshot.ID)
	if err != nil {
		return nil, fmt.Errorf("load match snapshot: query players: %w", err)
	}
	for players.Next() {
		var player playerSnapshot
		if err := players.Scan(&player.ID, &player.DisplayName, &player.Slot, &player.BestTestsPassed); err != nil {
			players.Close()
			return nil, fmt.Errorf("load match snapshot: scan player: %w", err)
		}
		snapshot.Players = append(snapshot.Players, player)
	}
	if err := players.Err(); err != nil {
		players.Close()
		return nil, fmt.Errorf("load match snapshot: iterate players: %w", err)
	}
	players.Close()
	if len(snapshot.Players) != 2 {
		return nil, fmt.Errorf("load match snapshot: match %s has %d players", snapshot.ID, len(snapshot.Players))
	}

	submissions, err := tx.Query(ctx, `
		SELECT id, request_id, language, status, result, failure_kind,
		       tests_passed, created_at, finished_at
		FROM submissions
		WHERE match_id = $1 AND player_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, snapshot.ID, userID, maxSnapshotSubmissions+1)
	if err != nil {
		return nil, fmt.Errorf("load match snapshot: query submissions: %w", err)
	}
	for submissions.Next() {
		var submission submissionSnapshot
		if err := submissions.Scan(
			&submission.ID,
			&submission.RequestID,
			&submission.Language,
			&submission.Status,
			&submission.Verdict,
			&submission.FailureKind,
			&submission.TestsPassed,
			&submission.CreatedAt,
			&submission.FinishedAt,
		); err != nil {
			submissions.Close()
			return nil, fmt.Errorf("load match snapshot: scan submission: %w", err)
		}
		snapshot.Submissions = append(snapshot.Submissions, submission)
	}
	if err := submissions.Err(); err != nil {
		submissions.Close()
		return nil, fmt.Errorf("load match snapshot: iterate submissions: %w", err)
	}
	submissions.Close()
	if len(snapshot.Submissions) > maxSnapshotSubmissions {
		snapshot.Submissions = snapshot.Submissions[:maxSnapshotSubmissions]
		snapshot.Truncated = true
	}
	slices.Reverse(snapshot.Submissions)

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("load match snapshot: commit transaction: %w", err)
	}
	return snapshot, nil
}

func setSnapshotOutcome(snapshot *matchSnapshot, userID uuid.UUID) error {
	switch snapshot.Status {
	case "active":
		return nil
	case "finished":
		outcome := "draw"
		if snapshot.WinnerID != nil {
			outcome = "loss"
			if *snapshot.WinnerID == userID {
				outcome = "win"
			}
		}
		snapshot.Outcome = &outcome
		return nil
	default:
		return fmt.Errorf("load match snapshot: unknown match status %q", snapshot.Status)
	}
}
