package judge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/nglong14/CodeDuel/internal/app"
	"github.com/nglong14/CodeDuel/internal/config"
	"github.com/nglong14/CodeDuel/internal/redisx"
)

var newExecutor = func(
	ctx context.Context,
	cfg config.JudgeConfig,
	logger *slog.Logger,
) (Executor, error) {
	return newExecutorForConfig(ctx, cfg, logger)
}

func newExecutorForConfig(
	ctx context.Context,
	cfg config.JudgeConfig,
	logger *slog.Logger,
) (Executor, error) {
	switch cfg.Executor {
	case "", config.JudgeExecutorDocker:
		return NewDockerExecutor(ctx, cfg, logger)
	case config.JudgeExecutorKubernetes:
		return NewKubernetesJobExecutor(ctx, cfg, logger)
	default:
		return nil, fmt.Errorf("unsupported JUDGE_EXECUTOR %q", cfg.Executor)
	}
}

func Run(ctx context.Context, deps *app.Dependencies) error {
	executor, err := newExecutor(ctx, deps.Config.Judge, deps.Logger)
	if err != nil {
		return fmt.Errorf("initialize sandbox executor: %w", err)
	}
	defer func() {
		if err := executor.Close(); err != nil {
			deps.Logger.Error("close sandbox executor", "err", err)
		}
	}()
	queue := redisx.NewJudgeQueue(deps.Redis)
	if err := queue.EnsureGroup(ctx); err != nil {
		return fmt.Errorf("initialize judge queue: %w", err)
	}
	store, err := newPostgresStore(deps.Postgres)
	if err != nil {
		return err
	}
	consumerPrefix := judgeConsumerName()
	workers := make([]*judgeService, deps.Config.Judge.Concurrency)
	for index := range workers {
		workers[index], err = newJudgeService(
			deps.Logger,
			queue,
			store,
			executor,
			func(callCtx context.Context, channel string, payload []byte) error {
				return deps.Redis.Publish(callCtx, channel, payload).Err()
			},
			fmt.Sprintf("%s-%d", consumerPrefix, index+1),
			deps.Config.Judge.AttemptLease,
			limitsFromConfig(deps.Config.Judge),
		)
		if err != nil {
			return err
		}
	}
	shutdownGrace := deps.Config.Judge.TotalTimeout + 2*deps.Config.Judge.CleanupTimeout

	deps.Logger.Info("ready",
		"sandbox_languages", 3,
		"sandbox_concurrency", len(workers),
		"sandbox_timeout", deps.Config.Judge.TotalTimeout,
		"shutdown_grace", shutdownGrace,
		"consumer_prefix", consumerPrefix,
	)
	err = runJudgeWorkers(ctx, workers, shutdownGrace)
	deps.Logger.Info("shutting down")
	return err
}

func runJudgeWorkers(ctx context.Context, workers []*judgeService, shutdownGrace time.Duration) error {
	if len(workers) == 0 || shutdownGrace <= 0 {
		return errors.New("run judge workers: invalid configuration")
	}
	readCtx, cancelReads := context.WithCancel(ctx)
	defer cancelReads()
	workCtx, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWork()

	done := make(chan error, len(workers))
	for _, worker := range workers {
		if worker == nil {
			return errors.New("run judge workers: nil worker")
		}
	}
	for _, worker := range workers {
		go func() {
			done <- worker.run(readCtx, workCtx)
		}()
	}

	remaining := len(workers)
	shutdownSignal := ctx.Done()
	var (
		firstErr  error
		forceStop <-chan time.Time
		timer     *time.Timer
	)
	for remaining > 0 {
		select {
		case err := <-done:
			remaining--
			if err != nil && !errors.Is(err, context.Canceled) && firstErr == nil {
				firstErr = err
				cancelReads()
				timer = time.NewTimer(shutdownGrace)
				forceStop = timer.C
				shutdownSignal = nil
			}
		case <-shutdownSignal:
			cancelReads()
			timer = time.NewTimer(shutdownGrace)
			forceStop = timer.C
			shutdownSignal = nil
		case <-forceStop:
			cancelWork()
			forceStop = nil
		}
	}
	if timer != nil && !timer.Stop() && forceStop != nil {
		<-timer.C
	}
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

func judgeConsumerName() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.NewString())
}
