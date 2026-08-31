package reaper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nglong14/CodeDuel/internal/config"
	"github.com/nglong14/CodeDuel/internal/redisx"
	"github.com/nglong14/CodeDuel/internal/submission"
)

const (
	advisoryLockSQL   = `SELECT pg_try_advisory_lock(hashtext('codeduel:reaper')::bigint)`
	advisoryUnlockSQL = `SELECT pg_advisory_unlock(hashtext('codeduel:reaper')::bigint)`
	unlockTimeout     = 2 * time.Second
)

type publisher func(context.Context, string, []byte) error

type service struct {
	logger     *slog.Logger
	pool       *pgxpool.Pool
	queue      *redisx.JudgeQueue
	dispatcher *submission.Dispatcher
	publish    publisher
	consumer   string
	cfg        config.ReaperConfig
}

func newService(
	logger *slog.Logger,
	pool *pgxpool.Pool,
	queue *redisx.JudgeQueue,
	dispatcher *submission.Dispatcher,
	publish publisher,
	consumer string,
	cfg config.ReaperConfig,
) (*service, error) {
	if logger == nil || pool == nil || queue == nil || dispatcher == nil || publish == nil || consumer == "" {
		return nil, errors.New("initialize reaper: missing dependency")
	}
	if cfg.Interval <= 0 || cfg.MaxAttempts <= 0 || cfg.BatchSize <= 0 || cfg.StreamMinIdle < 0 {
		return nil, errors.New("initialize reaper: invalid config")
	}
	return &service{
		logger:     logger,
		pool:       pool,
		queue:      queue,
		dispatcher: dispatcher,
		publish:    publish,
		consumer:   consumer,
		cfg:        cfg,
	}, nil
}

func (s *service) run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.tick(ctx); err != nil {
			s.logger.Warn("reaper tick failed", "err", err)
		}

		timer := time.NewTimer(s.cfg.Interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *service) tick(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return errors.New("reaper tick: missing dependency")
	}
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("reaper tick: acquire connection: %w", err)
	}
	defer conn.Release()

	var acquired bool
	if err := conn.QueryRow(ctx, advisoryLockSQL).Scan(&acquired); err != nil {
		return fmt.Errorf("reaper tick: try advisory lock: %w", err)
	}
	if !acquired {
		return nil
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, advisoryUnlockSQL); err != nil {
			s.logger.Warn("release reaper advisory lock failed", "err", err)
		}
	}()

	if err := s.reclaimLeases(ctx, conn); err != nil {
		return err
	}
	if err := s.sweepStream(ctx, conn); err != nil {
		return err
	}
	return s.finalizeMatches(ctx, conn)
}
