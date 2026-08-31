package reaper

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func decideTiebreakWinner(players [2]uuid.UUID, scores [2]int) uuid.NullUUID {
	if scores[0] > scores[1] {
		return uuid.NullUUID{UUID: players[0], Valid: true}
	}
	if scores[1] > scores[0] {
		return uuid.NullUUID{UUID: players[1], Valid: true}
	}
	return uuid.NullUUID{}
}

func (s *service) finalizeMatches(ctx context.Context, conn *pgxpool.Conn) error {
	if s == nil || conn == nil {
		return errors.New("finalize matches: missing dependency")
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("finalize matches: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id
		FROM matches
		WHERE status = 'active' AND deadline <= clock_timestamp()
		ORDER BY deadline, id
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, s.cfg.BatchSize)
	if err != nil {
		return fmt.Errorf("finalize matches: select expired: %w", err)
	}
	ids := make([]uuid.UUID, 0, s.cfg.BatchSize)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("finalize matches: scan match: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("finalize matches: iterate matches: %w", err)
	}
	rows.Close()

	var events []publishedEvent
	finalized := 0
	for _, matchID := range ids {
		matchEvents, err := s.finalizeMatch(ctx, tx, matchID)
		if err != nil {
			return err
		}
		if len(matchEvents) == 0 {
			continue
		}
		events = append(events, matchEvents...)
		finalized++
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("finalize matches: commit transaction: %w", err)
	}
	if finalized > 0 {
		s.logger.Info("finalized expired matches", "count", finalized)
	}
	return s.publishEvents(ctx, events)
}

func (s *service) finalizeMatch(ctx context.Context, tx pgx.Tx, matchID uuid.UUID) ([]publishedEvent, error) {
	var open int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM submissions
		WHERE match_id = $1 AND status IN ('pending', 'running')
	`, matchID).Scan(&open); err != nil {
		return nil, fmt.Errorf("finalize match %s: count open submissions: %w", matchID, err)
	}
	if open > 0 {
		return nil, nil
	}

	rows, err := tx.Query(ctx, `
		SELECT mp.user_id,
		       COALESCE(MAX(s.tests_passed) FILTER (WHERE s.status = 'completed'), 0)
		FROM match_players mp
		LEFT JOIN submissions s ON s.match_id = mp.match_id AND s.player_id = mp.user_id
		WHERE mp.match_id = $1
		GROUP BY mp.user_id, mp.slot
		ORDER BY mp.slot, mp.user_id
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("finalize match %s: load scores: %w", matchID, err)
	}
	var (
		players [2]uuid.UUID
		scores  [2]int
		count   int
	)
	for rows.Next() {
		if count >= len(players) {
			rows.Close()
			s.logger.Warn("skip expired match with unexpected player count", "match_id", matchID)
			return nil, nil
		}
		if err := rows.Scan(&players[count], &scores[count]); err != nil {
			rows.Close()
			return nil, fmt.Errorf("finalize match %s: scan player score: %w", matchID, err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("finalize match %s: iterate player scores: %w", matchID, err)
	}
	rows.Close()
	if count != len(players) || players[0] == uuid.Nil || players[1] == uuid.Nil || players[0] == players[1] {
		s.logger.Warn("skip expired match with invalid players", "match_id", matchID)
		return nil, nil
	}

	var totalTests int
	if err := tx.QueryRow(ctx, `
		SELECT jsonb_array_length(p.test_cases)
		FROM matches m
		JOIN problems p ON p.id = m.problem_id
		WHERE m.id = $1
	`, matchID).Scan(&totalTests); err != nil {
		return nil, fmt.Errorf("finalize match %s: load total tests: %w", matchID, err)
	}

	winner := decideTiebreakWinner(players, scores)
	tag, err := tx.Exec(ctx, `
		UPDATE matches
		SET status = 'finished', winner_id = $2
		WHERE id = $1 AND status = 'active' AND winner_id IS NULL
	`, matchID, winner)
	if err != nil {
		return nil, fmt.Errorf("finalize match %s: update match: %w", matchID, err)
	}
	if tag.RowsAffected() == 0 {
		return nil, nil
	}
	return buildMatchEndEvents(matchID, players, scores, totalTests, winner)
}
