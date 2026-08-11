package app

import (
	"log/slog"
	"os"

	"github.com/nglong14/CodeDuel/internal/config"
)

func NewLogger(cfg config.LogConfig) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Level),
	}

	var handler slog.Handler
	if cfg.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
