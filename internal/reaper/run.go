package reaper

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/nglong14/CodeDuel/internal/app"
	"github.com/nglong14/CodeDuel/internal/redisx"
	"github.com/nglong14/CodeDuel/internal/submission"
)

func Run(ctx context.Context, deps *app.Dependencies) error {
	queue := redisx.NewJudgeQueue(deps.Redis)
	if err := queue.EnsureGroup(ctx); err != nil {
		return fmt.Errorf("initialize judge queue: %w", err)
	}
	dispatcher, err := submission.NewDispatcher(
		deps.Postgres,
		queue,
		deps.Config.Match.SubmissionReenqueueAfter,
		submission.DefaultDispatchBatchSize,
	)
	if err != nil {
		return err
	}
	service, err := newService(
		deps.Logger,
		deps.Postgres,
		queue,
		dispatcher,
		func(callCtx context.Context, channel string, payload []byte) error {
			return deps.Redis.Publish(callCtx, channel, payload).Err()
		},
		reaperConsumerName(),
		deps.Config.Reaper,
	)
	if err != nil {
		return err
	}

	deps.Logger.Info("ready",
		"redis_addr", deps.Config.Redis.Addr,
		"interval", deps.Config.Reaper.Interval,
		"max_attempts", deps.Config.Reaper.MaxAttempts,
		"stream_min_idle", deps.Config.Reaper.StreamMinIdle,
		"batch_size", deps.Config.Reaper.BatchSize,
		"consumer", service.consumer,
	)
	err = service.run(ctx)
	deps.Logger.Info("shutting down")
	return err
}

func reaperConsumerName() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("reaper-%s-%d-%s", hostname, os.Getpid(), uuid.NewString())
}
