package reaper

import (
	"context"

	"github.com/nglong14/CodeDuel/internal/app"
)

func Run(ctx context.Context, deps *app.Dependencies) error {
	deps.Logger.Info("ready",
		"redis_addr", deps.Config.Redis.Addr,
		"match_duration", deps.Config.Match.Duration,
	)

	<-ctx.Done()
	deps.Logger.Info("shutting down")
	return ctx.Err()
}
