package match

import (
	"context"
	"errors"
	"fmt"

	"github.com/nglong14/CodeDuel/internal/app"
	"github.com/nglong14/CodeDuel/internal/redisx"
	"github.com/nglong14/CodeDuel/internal/submission"
)

func Run(ctx context.Context, deps *app.Dependencies) error {
	if deps.Config.Match.Duration.Milliseconds() <= 0 {
		return fmt.Errorf("match duration must be at least one millisecond")
	}
	dispatcher, err := submission.NewDispatcher(
		deps.Postgres,
		redisx.NewJudgeQueue(deps.Redis),
		deps.Config.Match.SubmissionReenqueueAfter,
		submission.DefaultDispatchBatchSize,
	)
	if err != nil {
		return err
	}
	queue := redisx.NewQueue(deps.Redis, redisx.DefaultScanLimit)
	service, err := newService(
		deps.Logger,
		queue,
		func(callCtx context.Context, players [2]redisx.QueueMember) (CreatedMatch, error) {
			return createMatch(callCtx, deps.Postgres, deps.Config.Match.Duration, players)
		},
		func(callCtx context.Context, route string, payload []byte) error {
			return deps.Redis.Publish(callCtx, route, payload).Err()
		},
	)
	if err != nil {
		return err
	}

	deps.Logger.Info("ready",
		"redis_addr", deps.Config.Redis.Addr,
		"match_duration", deps.Config.Match.Duration,
	)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	dispatchDone := make(chan error, 1)
	go func() {
		dispatchDone <- runSubmissionDispatcher(runCtx, deps.Logger, dispatcher, deps.Config.Match.SubmissionDispatchInterval)
	}()
	err = service.run(runCtx)
	cancel()
	dispatchErr := <-dispatchDone
	if err == nil && dispatchErr != nil && !errors.Is(dispatchErr, context.Canceled) {
		err = dispatchErr
	}
	deps.Logger.Info("shutting down")
	return err
}
