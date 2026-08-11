package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nglong14/CodeDuel/internal/app"
	"github.com/nglong14/CodeDuel/internal/config"
	"github.com/nglong14/CodeDuel/internal/gateway"
	"github.com/nglong14/CodeDuel/internal/judge"
	"github.com/nglong14/CodeDuel/internal/match"
	"github.com/nglong14/CodeDuel/internal/reaper"
)

func main() {
	roleFlag := flag.String("role", "", "service role: gateway, match, judge, or reaper")
	flag.Parse()

	if err := run(*roleFlag); err != nil {
		slog.Error("application exited", "role", *roleFlag, "err", err)
		os.Exit(1)
	}
}

func run(roleName string) error {
	app.NewLogger(config.LogConfig{})

	if roleName == "" {
		return errors.New("missing required --role flag (gateway|match|judge|reaper)")
	}
	if !validRole(roleName) {
		return fmt.Errorf("unknown role %q (expected gateway|match|judge|reaper)", roleName)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := app.NewLogger(cfg.Log).With("role", roleName)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	deps, err := app.NewDependencies(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("initialize dependencies: %w", err)
	}
	defer deps.Close()

	var runErr error
	switch roleName {
	case "gateway":
		runErr = gateway.Run(ctx, deps)
	case "match":
		runErr = match.Run(ctx, deps)
	case "judge":
		runErr = judge.Run(ctx, deps)
	case "reaper":
		runErr = reaper.Run(ctx, deps)
	}

	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return fmt.Errorf("run role: %w", runErr)
	}

	logger.Info("shutdown complete")
	return nil
}

func validRole(roleName string) bool {
	switch roleName {
	case "gateway", "match", "judge", "reaper":
		return true
	default:
		return false
	}
}
