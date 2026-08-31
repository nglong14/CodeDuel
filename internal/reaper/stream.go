package reaper

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nglong14/CodeDuel/internal/redisx"
)

func (s *service) sweepStream(ctx context.Context, conn *pgxpool.Conn) error {
	if s == nil || s.queue == nil || conn == nil {
		return errors.New("sweep stream: missing dependency")
	}
	jobs, _, err := s.queue.ReclaimIdle(ctx, s.consumer, s.cfg.StreamMinIdle, "0-0", int64(s.cfg.BatchSize))
	if err != nil {
		return fmt.Errorf("sweep stream: reclaim idle jobs: %w", err)
	}
	if len(jobs) == 0 {
		return nil
	}

	var firstErr error
	finalized := 0
	for _, job := range jobs {
		if err := s.handleReclaimedJob(ctx, conn, job); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.logger.Warn("reclaim stream job failed",
				"entry_id", job.EntryID,
				"submission_id", job.SubmissionID,
				"err", err,
			)
			continue
		}
		finalized++
	}
	if finalized > 0 {
		s.logger.Info("swept abandoned judge jobs", "count", finalized)
	}
	return firstErr
}

func (s *service) handleReclaimedJob(ctx context.Context, conn *pgxpool.Conn, job redisx.JudgeJob) error {
	if job.EntryID == "" {
		return errors.New("reclaim stream job: missing entry ID")
	}
	if job.DecodeErr != nil {
		s.logger.Warn("discard malformed reclaimed judge job", "entry_id", job.EntryID, "err", job.DecodeErr)
		return s.queue.Finalize(ctx, job.EntryID)
	}

	status, found, err := lookupSubmissionStatus(ctx, conn, job.SubmissionID)
	if err != nil {
		return err
	}
	if !found {
		s.logger.Warn("discard reclaimed job for missing submission",
			"entry_id", job.EntryID,
			"submission_id", job.SubmissionID,
		)
		return s.queue.Finalize(ctx, job.EntryID)
	}
	switch status {
	case "pending", "completed":
		if err := s.dispatcher.Dispatch(ctx, job.SubmissionID); err != nil {
			return fmt.Errorf("dispatch replacement job %s: %w", job.SubmissionID, err)
		}
		return s.queue.Finalize(ctx, job.EntryID)
	case "running":
		return nil
	default:
		return fmt.Errorf("reclaim stream job %s: invalid status %q", job.SubmissionID, status)
	}
}

func lookupSubmissionStatus(ctx context.Context, conn *pgxpool.Conn, submissionID uuid.UUID) (string, bool, error) {
	if conn == nil || submissionID == uuid.Nil {
		return "", false, errors.New("lookup submission status: invalid arguments")
	}
	var status string
	err := conn.QueryRow(ctx, `SELECT status FROM submissions WHERE id = $1`, submissionID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("lookup submission status: %w", err)
	}
	return status, true, nil
}
