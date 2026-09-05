package match

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nglong14/CodeDuel/internal/redisx"
)

type CreatedMatch struct {
	MatchID   uuid.UUID
	ProblemID uuid.UUID
	Deadline  time.Time
	Players   [2]redisx.QueueMember
}

type ActiveMatchClaim struct {
	UserID  uuid.UUID
	MatchID uuid.UUID
}

type ActiveMatchConflictError struct {
	Claims []ActiveMatchClaim
}

func (e *ActiveMatchConflictError) Error() string {
	return "one or more players already have an active match"
}

func (e *ActiveMatchConflictError) MatchFor(userID uuid.UUID) (uuid.UUID, bool) {
	for _, claim := range e.Claims {
		if claim.UserID == userID {
			return claim.MatchID, true
		}
	}
	return uuid.Nil, false
}

type MissingPlayersError struct {
	PlayerIDs []uuid.UUID
}

func (e *MissingPlayersError) Error() string {
	return "one or more players do not exist"
}

func (e *MissingPlayersError) Has(userID uuid.UUID) bool {
	return slices.Contains(e.PlayerIDs, userID)
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

	orderedIDs := []uuid.UUID{players[0].UserID, players[1].UserID}
	slices.SortFunc(orderedIDs, func(a, b uuid.UUID) int {
		return bytes.Compare(a[:], b[:])
	})
	missing := make([]uuid.UUID, 0, len(orderedIDs))
	for _, userID := range orderedIDs {
		var lockedID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&lockedID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				missing = append(missing, userID)
				continue
			}
			return CreatedMatch{}, fmt.Errorf("create match: lock player %s: %w", userID, err)
		}
	}

	claims, err := loadActiveMatchClaims(ctx, tx, orderedIDs)
	if err != nil {
		return CreatedMatch{}, err
	}
	if len(missing) > 0 || len(claims) > 0 {
		var causes []error
		if len(missing) > 0 {
			causes = append(causes, &MissingPlayersError{PlayerIDs: missing})
		}
		if len(claims) > 0 {
			causes = append(causes, &ActiveMatchConflictError{Claims: claims})
		}
		return CreatedMatch{}, fmt.Errorf("create match: ineligible players: %w", errors.Join(causes...))
	}

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
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.ConstraintName == "active_match_players_pkey" {
				_ = tx.Rollback(ctx)
				claims, loadErr := loadActiveMatchClaims(ctx, db, orderedIDs)
				if loadErr != nil {
					return CreatedMatch{}, loadErr
				}
				if len(claims) > 0 {
					return CreatedMatch{}, fmt.Errorf("create match: ineligible players: %w", &ActiveMatchConflictError{Claims: claims})
				}
			}
			return CreatedMatch{}, fmt.Errorf("create match: insert player %d: %w", i+1, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return CreatedMatch{}, fmt.Errorf("create match: commit: %w", err)
	}
	return result, nil
}

type activeClaimQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadActiveMatchClaims(ctx context.Context, db activeClaimQuerier, userIDs []uuid.UUID) ([]ActiveMatchClaim, error) {
	rows, err := db.Query(ctx, `
		SELECT user_id, match_id
		FROM active_match_players
		WHERE user_id = ANY($1)
		ORDER BY user_id
	`, userIDs)
	if err != nil {
		return nil, fmt.Errorf("create match: load active claims: %w", err)
	}
	defer rows.Close()

	claims := make([]ActiveMatchClaim, 0, len(userIDs))
	for rows.Next() {
		var claim ActiveMatchClaim
		if err := rows.Scan(&claim.UserID, &claim.MatchID); err != nil {
			return nil, fmt.Errorf("create match: scan active claim: %w", err)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("create match: iterate active claims: %w", err)
	}
	return claims, nil
}
