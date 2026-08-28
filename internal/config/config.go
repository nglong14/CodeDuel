package config

import (
	"fmt"
	"os"
	"strconv"
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
	Judge    JudgeConfig
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

type JudgeConfig struct {
	Concurrency     int
	MaxCodeBytes    int64
	MaxOutputBytes  int64
	CompileTimeout  time.Duration
	TestTimeout     time.Duration
	TotalTimeout    time.Duration
	CleanupTimeout  time.Duration
	AttemptLease    time.Duration
	NanoCPUs        int64
	MemoryBytes     int64
	MemorySwapBytes int64
	PIDLimit        int64
	WorkspaceBytes  int64
	TmpfsBytes      int64
	PythonImage     string
	CPPImage        string
	JavaImage       string
}

const judgeSetupMargin = 5 * time.Second

func (c JudgeConfig) Validate() error {
	if c.Concurrency <= 0 {
		return fmt.Errorf("JUDGE_CONCURRENCY must be positive")
	}
	if c.MaxCodeBytes < 64<<10 || c.MaxOutputBytes <= 0 {
		return fmt.Errorf("judge code limit must accept the 64 KiB protocol maximum and output limit must be positive")
	}
	if c.CompileTimeout <= 0 || c.TestTimeout <= 0 || c.TotalTimeout <= 0 || c.CleanupTimeout <= 0 {
		return fmt.Errorf("judge timeouts must be positive")
	}
	if c.CompileTimeout > c.TotalTimeout || c.TestTimeout > c.TotalTimeout {
		return fmt.Errorf("judge stage timeouts must not exceed the total timeout")
	}
	if c.AttemptLease <= c.TotalTimeout+2*c.CleanupTimeout+judgeSetupMargin {
		return fmt.Errorf("JUDGE_ATTEMPT_LEASE must cover total timeout, kill, cleanup, and setup margin")
	}
	if c.NanoCPUs <= 0 || c.MemoryBytes <= 0 || c.PIDLimit <= 0 || c.WorkspaceBytes <= 0 || c.TmpfsBytes <= 0 {
		return fmt.Errorf("judge resource limits must be positive")
	}
	if c.MemorySwapBytes < c.MemoryBytes {
		return fmt.Errorf("JUDGE_MEMORY_SWAP_BYTES must be at least JUDGE_MEMORY_BYTES")
	}
	if c.PythonImage == "" || c.CPPImage == "" || c.JavaImage == "" {
		return fmt.Errorf("judge image references must not be empty")
	}
	return nil
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
	judge, err := loadJudgeConfig()
	if err != nil {
		return nil, err
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
		Judge: judge,
	}

	return cfg, nil
}

func loadJudgeConfig() (JudgeConfig, error) {
	compileTimeout, err := positiveDuration("JUDGE_COMPILE_TIMEOUT", "10s")
	if err != nil {
		return JudgeConfig{}, err
	}
	testTimeout, err := positiveDuration("JUDGE_TEST_TIMEOUT", "2s")
	if err != nil {
		return JudgeConfig{}, err
	}
	totalTimeout, err := positiveDuration("JUDGE_TOTAL_TIMEOUT", "30s")
	if err != nil {
		return JudgeConfig{}, err
	}
	cleanupTimeout, err := positiveDuration("JUDGE_CLEANUP_TIMEOUT", "10s")
	if err != nil {
		return JudgeConfig{}, err
	}
	attemptLease, err := positiveDuration("JUDGE_ATTEMPT_LEASE", "1m")
	if err != nil {
		return JudgeConfig{}, err
	}

	cfg := JudgeConfig{
		CompileTimeout: compileTimeout,
		TestTimeout:    testTimeout,
		TotalTimeout:   totalTimeout,
		CleanupTimeout: cleanupTimeout,
		AttemptLease:   attemptLease,
		PythonImage:    envOr("JUDGE_PYTHON_IMAGE", "codeduel/sandbox-python:3.13"),
		CPPImage:       envOr("JUDGE_CPP_IMAGE", "codeduel/sandbox-cpp:gcc14"),
		JavaImage:      envOr("JUDGE_JAVA_IMAGE", "codeduel/sandbox-java:temurin21"),
	}
	values := []struct {
		name     string
		fallback string
		target   *int64
	}{
		{"JUDGE_MAX_CODE_BYTES", "65536", &cfg.MaxCodeBytes},
		{"JUDGE_MAX_OUTPUT_BYTES", "1048576", &cfg.MaxOutputBytes},
		{"JUDGE_CPU_NANOS", "1000000000", &cfg.NanoCPUs},
		{"JUDGE_MEMORY_BYTES", "268435456", &cfg.MemoryBytes},
		{"JUDGE_MEMORY_SWAP_BYTES", "268435456", &cfg.MemorySwapBytes},
		{"JUDGE_PID_LIMIT", "64", &cfg.PIDLimit},
		{"JUDGE_WORKSPACE_BYTES", "67108864", &cfg.WorkspaceBytes},
		{"JUDGE_TMPFS_BYTES", "16777216", &cfg.TmpfsBytes},
	}
	for _, value := range values {
		parsed, parseErr := positiveInt64(value.name, value.fallback)
		if parseErr != nil {
			return JudgeConfig{}, parseErr
		}
		*value.target = parsed
	}
	concurrency, err := positiveInt64("JUDGE_CONCURRENCY", "2")
	if err != nil {
		return JudgeConfig{}, err
	}
	cfg.Concurrency = int(concurrency)
	if err := cfg.Validate(); err != nil {
		return JudgeConfig{}, fmt.Errorf("validate Judge config: %w", err)
	}
	return cfg, nil
}

func positiveDuration(name, fallback string) (time.Duration, error) {
	value, err := time.ParseDuration(envOr(name, fallback))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("parse %s: must be a positive duration", name)
	}
	return value, nil
}

func positiveInt64(name, fallback string) (int64, error) {
	value, err := strconv.ParseInt(envOr(name, fallback), 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("parse %s: must be a positive integer", name)
	}
	return value, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
