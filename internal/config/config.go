package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Postgres PostgresConfig
	Redis    RedisConfig
	Match    MatchConfig
	Log      LogConfig
	Gateway  GatewayConfig
}

type PostgresConfig struct {
	DSN string
}

type RedisConfig struct {
	Addr string
}

type MatchConfig struct {
	Duration                   time.Duration
	SubmissionDispatchInterval time.Duration
	SubmissionReenqueueAfter   time.Duration
}

type LogConfig struct {
	Level  string
	Format string
}

type GatewayConfig struct {
	Addr      string
	JWTSecret string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	matchDuration, err := time.ParseDuration(envOr("MATCH_DURATION", "10m"))
	if err != nil {
		return nil, fmt.Errorf("parse MATCH_DURATION: %w", err)
	}
	dispatchInterval, err := time.ParseDuration(envOr("SUBMISSION_DISPATCH_INTERVAL", "5s"))
	if err != nil || dispatchInterval <= 0 {
		return nil, fmt.Errorf("parse SUBMISSION_DISPATCH_INTERVAL: must be a positive duration")
	}
	reenqueueAfter, err := time.ParseDuration(envOr("SUBMISSION_REENQUEUE_AFTER", "30s"))
	if err != nil || reenqueueAfter <= 0 {
		return nil, fmt.Errorf("parse SUBMISSION_REENQUEUE_AFTER: must be a positive duration")
	}

	cfg := &Config{
		Postgres: PostgresConfig{
			DSN: envOr("POSTGRES_DSN", "postgres://codeduel:codeduel@localhost:5433/codeduel?sslmode=disable"),
		},
		Redis: RedisConfig{
			Addr: envOr("REDIS_ADDR", "localhost:6379"),
		},
		Match: MatchConfig{
			Duration:                   matchDuration,
			SubmissionDispatchInterval: dispatchInterval,
			SubmissionReenqueueAfter:   reenqueueAfter,
		},
		Log: LogConfig{
			Level:  strings.ToLower(envOr("LOG_LEVEL", "info")),
			Format: strings.ToLower(envOr("LOG_FORMAT", "text")),
		},
		Gateway: GatewayConfig{
			Addr:      envOr("GATEWAY_ADDR", ":8080"),
			JWTSecret: envOr("JWT_SECRET", "codeduel-dev-secret"),
		},
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
