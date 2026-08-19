package match

import (
	"context"
	"fmt"

	"github.com/nglong14/CodeDuel/internal/app"
	"github.com/nglong14/CodeDuel/internal/redisx"
)

func Run(ctx context.Context, deps *app.Dependencies) error {
	if deps.Config.Match.Duration.Milliseconds() <= 0 {
		return fmt.Errorf("match duration must be at least one millisecond")
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
	err = service.run(ctx)
	deps.Logger.Info("shutting down")
	return err
}
