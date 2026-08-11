package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nglong14/CodeDuel/internal/config"
	"github.com/nglong14/CodeDuel/internal/infrastructure"
	"github.com/redis/go-redis/v9"
)

type Dependencies struct {
	Config   *config.Config
	Logger   *slog.Logger
	Postgres *pgxpool.Pool
	Redis    *redis.Client
}

func NewDependencies(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
) (*Dependencies, error) {
	postgres, err := infrastructure.NewPostgres(ctx, cfg.Postgres.DSN)
	if err != nil {
		return nil, fmt.Errorf("initialize postgres: %w", err)
	}

	redisClient, err := infrastructure.NewRedis(ctx, cfg.Redis.Addr)
	if err != nil {
		postgres.Close()
		return nil, fmt.Errorf("initialize redis: %w", err)
	}

	return &Dependencies{
		Config:   cfg,
		Logger:   logger,
		Postgres: postgres,
		Redis:    redisClient,
	}, nil
}

func (d *Dependencies) Close() {
	_ = d.Redis.Close()
	d.Postgres.Close()
}
