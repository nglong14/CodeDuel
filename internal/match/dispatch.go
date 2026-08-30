package match

import (
	"context"
	"log/slog"
	"time"

	"github.com/nglong14/CodeDuel/internal/submission"
)

func runSubmissionDispatcher(ctx context.Context, logger *slog.Logger, dispatcher *submission.Dispatcher, interval time.Duration) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if count, err := dispatcher.DispatchPending(ctx); err != nil {
			logger.Warn("dispatch pending submissions failed", "err", err)
		} else if count > 0 {
			logger.Info("dispatched pending submissions", "count", count)
		}

		timer := time.NewTimer(interval)
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
