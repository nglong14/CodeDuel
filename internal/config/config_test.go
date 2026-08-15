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
