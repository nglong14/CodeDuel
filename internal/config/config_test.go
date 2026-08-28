package config

import (
	"testing"
	"time"
)

func TestLoadGatewayDefaults(t *testing.T) {
	t.Setenv("GATEWAY_ADDR", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("MATCH_DURATION", "10m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Gateway.Addr != ":8080" {
		t.Fatalf("Addr = %q, want :8080", cfg.Gateway.Addr)
	}
	if cfg.Gateway.JWTSecret != "codeduel-dev-secret" {
		t.Fatalf("JWTSecret = %q, want codeduel-dev-secret", cfg.Gateway.JWTSecret)
	}
}

func TestLoadGatewayFromEnv(t *testing.T) {
	t.Setenv("GATEWAY_ADDR", "127.0.0.1:9090")
	t.Setenv("JWT_SECRET", "prod-secret")
	t.Setenv("MATCH_DURATION", "10m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Gateway.Addr != "127.0.0.1:9090" {
		t.Fatalf("Addr = %q, want 127.0.0.1:9090", cfg.Gateway.Addr)
	}
	if cfg.Gateway.JWTSecret != "prod-secret" {
		t.Fatalf("JWTSecret = %q, want prod-secret", cfg.Gateway.JWTSecret)
	}
}

func TestLoadSubmissionDispatchConfig(t *testing.T) {
	t.Setenv("MATCH_DURATION", "10m")
	t.Setenv("SUBMISSION_DISPATCH_INTERVAL", "2s")
	t.Setenv("SUBMISSION_REENQUEUE_AFTER", "45s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Match.SubmissionDispatchInterval.String() != "2s" {
		t.Fatalf("SubmissionDispatchInterval = %v", cfg.Match.SubmissionDispatchInterval)
	}
	if cfg.Match.SubmissionReenqueueAfter.String() != "45s" {
		t.Fatalf("SubmissionReenqueueAfter = %v", cfg.Match.SubmissionReenqueueAfter)
	}
}

func TestLoadRejectsInvalidSubmissionDispatchConfig(t *testing.T) {
	t.Setenv("MATCH_DURATION", "10m")
	t.Setenv("SUBMISSION_DISPATCH_INTERVAL", "0s")
	if _, err := Load(); err == nil {
		t.Fatal("Load returned nil error for zero dispatch interval")
	}
}

func TestLoadJudgeDefaults(t *testing.T) {
	t.Setenv("JUDGE_CONCURRENCY", "")
	t.Setenv("JUDGE_MEMORY_BYTES", "")
	t.Setenv("JUDGE_MEMORY_SWAP_BYTES", "")
	t.Setenv("JUDGE_ATTEMPT_LEASE", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Judge.Concurrency != 2 || cfg.Judge.MemoryBytes != 256<<20 {
		t.Fatalf("Judge defaults = %#v", cfg.Judge)
	}
	if cfg.Judge.AttemptLease <= cfg.Judge.TotalTimeout+2*cfg.Judge.CleanupTimeout+judgeSetupMargin {
		t.Fatalf("attempt lease %v does not cover execution and cleanup", cfg.Judge.AttemptLease)
	}
}

func TestLoadRejectsInvalidJudgeConfig(t *testing.T) {
	t.Setenv("JUDGE_MEMORY_BYTES", "268435456")
	t.Setenv("JUDGE_MEMORY_SWAP_BYTES", "134217728")
	if _, err := Load(); err == nil {
		t.Fatal("Load returned nil error for swap below memory")
	}
}

func TestJudgeConfigRejectsShortLeaseAndCodeLimit(t *testing.T) {
	cfg, err := loadJudgeConfig()
	if err != nil {
		t.Fatalf("loadJudgeConfig: %v", err)
	}
	cfg.AttemptLease = cfg.TotalTimeout + 2*cfg.CleanupTimeout
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate returned nil error for short attempt lease")
	}
	cfg.AttemptLease = time.Minute
	cfg.MaxCodeBytes = 1024
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate returned nil error for code limit below protocol maximum")
	}
}
