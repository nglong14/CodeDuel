package judge

import (
	"context"
	"fmt"

	"github.com/nglong14/CodeDuel/internal/app"
)

func Run(ctx context.Context, deps *app.Dependencies) error {
	executor, err := NewDockerExecutor(ctx, deps.Config.Judge, deps.Logger)
	if err != nil {
		return fmt.Errorf("initialize sandbox executor: %w", err)
	}
	defer func() {
		if err := executor.Close(); err != nil {
			deps.Logger.Error("close Docker client", "err", err)
		}
	}()

	deps.Logger.Info("ready",
		"sandbox_languages", 3,
		"sandbox_concurrency", deps.Config.Judge.Concurrency,
		"sandbox_timeout", deps.Config.Judge.TotalTimeout,
	)

	<-ctx.Done()
	deps.Logger.Info("shutting down")
	return ctx.Err()
}
