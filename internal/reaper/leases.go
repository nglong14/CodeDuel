package reaper

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type leaseAction struct {
	Kind         string
	SubmissionID uuid.UUID
	MatchID      uuid.UUID
	PlayerID     uuid.UUID
	TotalTests   int
}

func (s *service) reclaimLeases(ctx context.Context, conn *pgxpool.Conn) error {
	if s == nil || conn == nil {
		return fmt.Errorf("reclaim leases: missing dependency")
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("reclaim leases: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		WITH expired AS (
			SELECT id, attempts
			FROM submissions
			WHERE status = 'running'
			  AND (lease_until IS NULL OR lease_until <= clock_timestamp())
			ORDER BY lease_until NULLS FIRST, id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		),
		reset AS (
			UPDATE submissions s
			SET status = 'pending',
			    attempt_token = NULL,
			    lease_until = NULL,
			    last_enqueued_at = NULL
			FROM expired e
			WHERE s.id = e.id AND e.attempts < $2
			RETURNING s.id
		),
		poisoned AS (
			UPDATE submissions s
			SET status = 'completed',
			    result = 'failed',
			    failure_kind = 'infrastructure_error',
			    attempt_token = NULL,
			    lease_until = NULL,
			    finished_at = clock_timestamp()
			FROM expired e
			WHERE s.id = e.id AND e.attempts >= $2
			RETURNING s.id, s.match_id, s.player_id
		)
		SELECT 'reset', id, NULL::uuid, NULL::uuid, 0
		FROM reset
		UNION ALL
		SELECT 'poisoned', p.id, p.match_id, p.player_id, jsonb_array_length(pr.test_cases)
		FROM poisoned p
		JOIN matches m ON m.id = p.match_id
		JOIN problems pr ON pr.id = m.problem_id
	`, s.cfg.BatchSize, s.cfg.MaxAttempts)
	if err != nil {
		return fmt.Errorf("reclaim leases: select expired: %w", err)
	}

	var actions []leaseAction
	for rows.Next() {
		var action leaseAction
		var matchID, playerID uuid.NullUUID
		if err := rows.Scan(&action.Kind, &action.SubmissionID, &matchID, &playerID, &action.TotalTests); err != nil {
			rows.Close()
			return fmt.Errorf("reclaim leases: scan action: %w", err)
		}
		if matchID.Valid {
			action.MatchID = matchID.UUID
		}
		if playerID.Valid {
			action.PlayerID = playerID.UUID
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("reclaim leases: iterate actions: %w", err)
	}
	rows.Close()

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reclaim leases: commit transaction: %w", err)
	}

	reset := 0
	var events []publishedEvent
	for _, action := range actions {
		switch action.Kind {
		case "reset":
			reset++
		case "poisoned":
			event, err := buildFailedResultEvent(action.SubmissionID, action.MatchID, action.PlayerID, action.TotalTests)
			if err != nil {
				return err
			}
			events = append(events, event)
		default:
			return fmt.Errorf("reclaim leases: unknown action %q", action.Kind)
		}
	}
	if reset > 0 || len(events) > 0 {
		s.logger.Info("reclaimed expired leases", "reset", reset, "poisoned", len(events))
	}
	return s.publishEvents(ctx, events)
}
