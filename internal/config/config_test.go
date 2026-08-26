package config

import "testing"

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
