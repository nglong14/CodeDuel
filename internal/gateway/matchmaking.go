package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nglong14/CodeDuel/internal/redisx"
)

type matchmakingQueue interface {
	Enqueue(context.Context, redisx.QueueMember) (redisx.EnqueueResult, error)
}

type alreadyInActiveMatchError struct {
	matchID uuid.UUID
}

func (e *alreadyInActiveMatchError) Error() string {
	return "player already has an active match"
}

func enqueueForMatch(
	ctx context.Context,
	db *pgxpool.Pool,
	queue matchmakingQueue,
	member redisx.QueueMember,
) error {
	if db == nil || queue == nil {
		return errors.New("enqueue for match: missing dependency")
	}
	if err := member.Validate(); err != nil {
		return fmt.Errorf("enqueue for match: %w", err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("enqueue for match: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, member.UserID).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("enqueue for match: user not found")
		}
		return fmt.Errorf("enqueue for match: lock user: %w", err)
	}

	var matchID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT match_id FROM active_match_players WHERE user_id = $1`, member.UserID).Scan(&matchID)
	switch {
	case err == nil:
		return &alreadyInActiveMatchError{matchID: matchID}
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("enqueue for match: load active match: %w", err)
	}

	if _, err := queue.Enqueue(ctx, member); err != nil {
		return fmt.Errorf("enqueue for match: %w", err)
	}
	// The transaction has no writes; rollback releases the user lock after Redis admission.
	return nil
}
