package match

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nglong14/CodeDuel/internal/redisx"
)

type CreatedMatch struct {
	MatchID   uuid.UUID
	ProblemID uuid.UUID
	Deadline  time.Time
	Players   [2]redisx.QueueMember
}

func createMatch(
	ctx context.Context,
	db *pgxpool.Pool,
	duration time.Duration,
	players [2]redisx.QueueMember,
) (CreatedMatch, error) {
	if duration.Milliseconds() <= 0 {
		return CreatedMatch{}, fmt.Errorf("create match: duration must be at least one millisecond")
	}
	for i, player := range players {
		if err := player.Validate(); err != nil {
			return CreatedMatch{}, fmt.Errorf("create match: invalid player %d: %w", i+1, err)
		}
	}
	if players[0].UserID == players[1].UserID {
		return CreatedMatch{}, fmt.Errorf("create match: players must be distinct")
	}
	if db == nil {
		return CreatedMatch{}, fmt.Errorf("create match: missing database")
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return CreatedMatch{}, fmt.Errorf("create match: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var result CreatedMatch
	result.Players = players
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM problems
		ORDER BY created_at, id
		LIMIT 1
	`).Scan(&result.ProblemID); err != nil {
		return CreatedMatch{}, fmt.Errorf("create match: select problem: %w", err)
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO matches (problem_id, status, deadline)
		VALUES ($1, 'active', now() + ($2 * interval '1 millisecond'))
		RETURNING id, deadline
	`, result.ProblemID, duration.Milliseconds()).Scan(&result.MatchID, &result.Deadline); err != nil {
		return CreatedMatch{}, fmt.Errorf("create match: insert match: %w", err)
	}

	for i, player := range players {
		if _, err := tx.Exec(ctx, `
			INSERT INTO match_players (match_id, user_id, slot)
			VALUES ($1, $2, $3)
		`, result.MatchID, player.UserID, i+1); err != nil {
			return CreatedMatch{}, fmt.Errorf("create match: insert player %d: %w", i+1, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return CreatedMatch{}, fmt.Errorf("create match: commit: %w", err)
	}
	return result, nil
}
