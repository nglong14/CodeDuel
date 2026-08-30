// Package submission owns durable submission admission rules.
package submission

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidRequest      = errors.New("invalid submission request")
	ErrMatchNotFound       = errors.New("match not found")
	ErrNotMatchPlayer      = errors.New("not a match player")
	ErrMatchNotActive      = errors.New("match not active")
	ErrDeadlinePassed      = errors.New("match deadline passed")
	ErrIdempotencyConflict = errors.New("submission idempotency conflict")
)

type Request struct {
	PlayerID  uuid.UUID
	MatchID   uuid.UUID
	RequestID uuid.UUID
	Language  string
	Code      string
}

type Service struct {
	db         *pgxpool.Pool
	dispatcher immediateDispatcher
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func NewServiceWithDispatcher(db *pgxpool.Pool, dispatcher immediateDispatcher) *Service {
	return &Service{db: db, dispatcher: dispatcher}
}

// Accept records a pending submission after applying membership and deadline rules.
func (s *Service) Accept(ctx context.Context, request Request) (uuid.UUID, error) {
	if s == nil || s.db == nil || !request.valid() {
		return uuid.Nil, ErrInvalidRequest
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin submission transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Serialize one player/request pair so concurrent retries can resolve the same row.
	if _, err := tx.Exec(ctx, `
		SELECT pg_advisory_xact_lock(hashtext($1::text), hashtext($2::text))
	`, request.PlayerID, request.RequestID); err != nil {
		return uuid.Nil, fmt.Errorf("lock submission request: %w", err)
	}

	id, found, err := findRequest(ctx, tx, request)
	if err != nil {
		return uuid.Nil, err
	}
	if found {
		if id == uuid.Nil {
			return uuid.Nil, ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, fmt.Errorf("commit idempotent submission: %w", err)
		}
		return id, nil
	}

	var status string
	var deadline, now time.Time
	if err := tx.QueryRow(ctx, `
		SELECT status, deadline, clock_timestamp()
		FROM matches
		WHERE id = $1
		FOR UPDATE
	`, request.MatchID).Scan(&status, &deadline, &now); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrMatchNotFound
		}
		return uuid.Nil, fmt.Errorf("lock match: %w", err)
	}

	var member bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM match_players WHERE match_id = $1 AND user_id = $2
		)
	`, request.MatchID, request.PlayerID).Scan(&member); err != nil {
		return uuid.Nil, fmt.Errorf("verify match player: %w", err)
	}
	if !member {
		return uuid.Nil, ErrNotMatchPlayer
	}
	if status != "active" {
		return uuid.Nil, ErrMatchNotActive
	}
	if !now.Before(deadline) {
		return uuid.Nil, ErrDeadlinePassed
	}

	if err := tx.QueryRow(ctx, `
		INSERT INTO submissions (match_id, player_id, request_id, language, code)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, request.MatchID, request.PlayerID, request.RequestID, request.Language, request.Code).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("insert submission: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit submission: %w", err)
	}
	if s.dispatcher != nil {
		// The submission is durable even when Redis is unavailable; Match will recover it.
		_ = s.dispatcher.Dispatch(ctx, id)
	}
	return id, nil
}

func findRequest(ctx context.Context, tx pgx.Tx, request Request) (uuid.UUID, bool, error) {
	var id, matchID uuid.UUID
	var language, code string
	err := tx.QueryRow(ctx, `
		SELECT id, match_id, language, code
		FROM submissions
		WHERE player_id = $1 AND request_id = $2
	`, request.PlayerID, request.RequestID).Scan(&id, &matchID, &language, &code)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("find idempotent submission: %w", err)
	}
	if matchID != request.MatchID || language != request.Language || code != request.Code {
		return uuid.Nil, true, nil
	}
	return id, true, nil
}

func (r Request) valid() bool {
	if r.PlayerID == uuid.Nil || r.MatchID == uuid.Nil || r.RequestID == uuid.Nil || r.Code == "" {
		return false
	}
	switch r.Language {
	case "python", "cpp", "java":
		return true
	default:
		return false
	}
}
