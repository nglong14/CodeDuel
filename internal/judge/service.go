package judge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nglong14/CodeDuel/internal/redisx"
)

const (
	judgeReadBlock     = 250 * time.Millisecond
	judgeRetryInterval = 500 * time.Millisecond
)

var errCompletionOwnershipLost = errors.New("submission completion ownership lost")

type judgeQueue interface {
	Read(context.Context, string, int64, time.Duration) ([]redisx.JudgeJob, error)
	Ack(context.Context, string) error
	Delete(context.Context, string) error
}

type judgeService struct {
	logger        *slog.Logger
	queue         judgeQueue
	store         submissionStore
	executor      Executor
	publish       func(context.Context, string, []byte) error
	consumer      string
	lease         time.Duration
	limits        Limits
	retryInterval time.Duration
}

func newJudgeService(
	logger *slog.Logger,
	queue judgeQueue,
	store submissionStore,
	executor Executor,
	publish func(context.Context, string, []byte) error,
	consumer string,
	lease time.Duration,
	limits Limits,
) (*judgeService, error) {
	if logger == nil || queue == nil || store == nil || executor == nil || publish == nil || consumer == "" || lease <= 0 {
		return nil, errors.New("initialize judge service: missing dependency")
	}
	if err := limits.Validate(); err != nil {
		return nil, fmt.Errorf("initialize judge service limits: %w", err)
	}
	return &judgeService{
		logger:        logger,
		queue:         queue,
		store:         store,
		executor:      executor,
		publish:       publish,
		consumer:      consumer,
		lease:         lease,
		limits:        limits,
		retryInterval: judgeRetryInterval,
	}, nil
}

func (s *judgeService) run(readCtx, workCtx context.Context) error {
	for {
		if err := readCtx.Err(); err != nil {
			return err
		}
		jobs, err := s.queue.Read(readCtx, s.consumer, 1, judgeReadBlock)
		if err != nil {
			if readCtx.Err() != nil {
				return readCtx.Err()
			}
			s.logger.Error("read judge jobs failed", "consumer", s.consumer, "err", err)
			if err := waitJudgeRetry(readCtx, s.retryInterval); err != nil {
				return err
			}
			continue
		}
		for _, job := range jobs {
			if err := s.processJob(workCtx, job); err != nil {
				s.logger.Error("process judge job failed",
					"consumer", s.consumer,
					"entry_id", job.EntryID,
					"submission_id", job.SubmissionID,
					"err", err,
				)
			}
		}
	}
}

func (s *judgeService) processJob(ctx context.Context, job redisx.JudgeJob) error {
	if job.EntryID == "" {
		return errors.New("process judge job: missing entry ID")
	}
	if job.DecodeErr != nil {
		s.logger.Warn("discard malformed judge job", "entry_id", job.EntryID, "err", job.DecodeErr)
		return s.ackAndDelete(ctx, job.EntryID)
	}

	attemptToken := uuid.New()
	claim, err := s.store.Claim(ctx, job.SubmissionID, attemptToken, s.lease)
	if err != nil {
		return fmt.Errorf("claim submission %s: %w", job.SubmissionID, err)
	}
	switch claim.Kind {
	case claimMissing:
		s.logger.Warn("discard judge job for missing submission", "entry_id", job.EntryID, "submission_id", job.SubmissionID)
		return s.ackAndDelete(ctx, job.EntryID)
	case claimRunning:
		s.logger.Info("discard duplicate judge job owned by live attempt",
			"entry_id", job.EntryID,
			"submission_id", job.SubmissionID,
			"lease_until", claim.LeaseUntil,
		)
		return s.ackAndDelete(ctx, job.EntryID)
	case claimExpired:
		s.logger.Warn("leave expired judge attempt for reaper", "entry_id", job.EntryID, "submission_id", job.SubmissionID)
		return nil
	case claimCompleted:
		return s.publishAckDelete(ctx, job.EntryID, claim.Completed)
	case claimAcquired:
	default:
		return fmt.Errorf("claim submission %s: unknown disposition %d", job.SubmissionID, claim.Kind)
	}

	request := ExecutionRequest{
		Language: claim.Claimed.Language,
		Source:   claim.Claimed.Source,
		Tests:    claim.Claimed.Tests,
		Limits:   s.limits,
	}
	outcome, err := s.executor.Execute(ctx, request)
	if err != nil {
		return fmt.Errorf("execute submission %s: %w", job.SubmissionID, err)
	}
	terminal, err := terminalResultFromOutcome(outcome, len(claim.Claimed.Tests))
	if err != nil {
		return fmt.Errorf("classify submission %s: %w", job.SubmissionID, err)
	}
	completion, err := s.store.Complete(ctx, claim.Claimed, terminal)
	if err != nil {
		return fmt.Errorf("complete submission %s: %w", job.SubmissionID, err)
	}
	if completion.Kind != completionApplied {
		return fmt.Errorf("complete submission %s: %w", job.SubmissionID, errCompletionOwnershipLost)
	}
	s.logger.Info("submission completed",
		"submission_id", completion.Completed.SubmissionID,
		"match_id", completion.Completed.MatchID,
		"verdict", completion.Completed.Verdict,
		"tests_passed", completion.Completed.TestsPassed,
		"winner_id", completion.Completed.WinnerID,
	)
	return s.publishAckDelete(ctx, job.EntryID, completion.Completed)
}

func (s *judgeService) publishAckDelete(ctx context.Context, entryID string, completed completedSubmission) error {
	events, err := buildResultEvents(completed)
	if err != nil {
		return err
	}
	var publishErrors []error
	for _, event := range events {
		if err := s.publish(ctx, redisx.UserChannel(event.RecipientID), event.Payload); err != nil {
			publishErrors = append(publishErrors, fmt.Errorf("publish result to %s: %w", event.RecipientID, err))
		}
	}
	if len(publishErrors) > 0 {
		return errors.Join(publishErrors...)
	}
	return s.ackAndDelete(ctx, entryID)
}

func (s *judgeService) ackAndDelete(ctx context.Context, entryID string) error {
	if err := s.queue.Ack(ctx, entryID); err != nil {
		return err
	}
	if err := s.queue.Delete(ctx, entryID); err != nil {
		return err
	}
	return nil
}

func waitJudgeRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
