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
	t.Setenv("JUDGE_SANDBOX_RUNTIME", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Judge.Concurrency != 2 || cfg.Judge.MemoryBytes != 256<<20 {
		t.Fatalf("Judge defaults = %#v", cfg.Judge)
	}
	if cfg.Judge.SandboxRuntime != "" {
		t.Fatalf("SandboxRuntime = %q, want empty", cfg.Judge.SandboxRuntime)
	}
	if cfg.Judge.AttemptLease <= cfg.Judge.TotalTimeout+2*cfg.Judge.CleanupTimeout+judgeSetupMargin {
		t.Fatalf("attempt lease %v does not cover execution and cleanup", cfg.Judge.AttemptLease)
	}
	if cfg.Judge.Executor != JudgeExecutorDocker {
		t.Fatalf("Executor = %q, want %q", cfg.Judge.Executor, JudgeExecutorDocker)
	}
	if cfg.Judge.K8sRuntimeClass != "gvisor" {
		t.Fatalf("K8sRuntimeClass = %q, want gvisor", cfg.Judge.K8sRuntimeClass)
	}
	if got := cfg.Judge.K8sNodeSelector; len(got) != 1 || got["sandbox"] != "true" {
		t.Fatalf("K8sNodeSelector = %v, want sandbox=true", got)
	}
}

func TestLoadJudgeKubernetesExecutor(t *testing.T) {
	t.Setenv("JUDGE_EXECUTOR", "kubernetes")
	t.Setenv("JUDGE_K8S_NAMESPACE", "codeduel-dev")
	t.Setenv("JUDGE_K8S_RUNTIME_CLASS", "gvisor")
	t.Setenv("JUDGE_K8S_NODE_SELECTOR", "sandbox=true, tier=judge")
	t.Setenv("JUDGE_K8S_TOLERATION_KEY", "sandbox")
	t.Setenv("JUDGE_K8S_TOLERATION_VALUE", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	judge := cfg.Judge
	if judge.Executor != JudgeExecutorKubernetes || judge.K8sNamespace != "codeduel-dev" {
		t.Fatalf("kubernetes executor config = %#v", judge)
	}
	if len(judge.K8sNodeSelector) != 2 || judge.K8sNodeSelector["sandbox"] != "true" || judge.K8sNodeSelector["tier"] != "judge" {
		t.Fatalf("K8sNodeSelector = %v", judge.K8sNodeSelector)
	}
}

func TestLoadRejectsKubernetesExecutorWithoutNamespace(t *testing.T) {
	t.Setenv("JUDGE_EXECUTOR", "kubernetes")
	t.Setenv("JUDGE_K8S_NAMESPACE", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted kubernetes executor without a namespace")
	}
}

func TestLoadRejectsUnknownJudgeExecutor(t *testing.T) {
	t.Setenv("JUDGE_EXECUTOR", "podman")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted an unknown JUDGE_EXECUTOR")
	}
}

func TestLoadRejectsMalformedNodeSelector(t *testing.T) {
	t.Setenv("JUDGE_EXECUTOR", "kubernetes")
	t.Setenv("JUDGE_K8S_NAMESPACE", "codeduel-dev")
	t.Setenv("JUDGE_K8S_NODE_SELECTOR", "sandbox")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a malformed JUDGE_K8S_NODE_SELECTOR")
	}
}

func TestJudgeConfigValidateKubernetesRequiresRuntimeClass(t *testing.T) {
	cfg, err := loadJudgeConfig()
	if err != nil {
		t.Fatalf("loadJudgeConfig: %v", err)
	}
	cfg.Executor = JudgeExecutorKubernetes
	cfg.K8sNamespace = "codeduel-dev"
	cfg.K8sRuntimeClass = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate accepted kubernetes executor without a runtime class")
	}
	cfg.K8sRuntimeClass = "gvisor"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadRejectsInvalidJudgeConfig(t *testing.T) {
	t.Setenv("JUDGE_MEMORY_BYTES", "268435456")
	t.Setenv("JUDGE_MEMORY_SWAP_BYTES", "134217728")
	if _, err := Load(); err == nil {
		t.Fatal("Load returned nil error for swap below memory")
	}
}

func TestLoadReaperDefaults(t *testing.T) {
	t.Setenv("REAPER_INTERVAL", "")
	t.Setenv("REAPER_MAX_ATTEMPTS", "")
	t.Setenv("REAPER_STREAM_MIN_IDLE", "")
	t.Setenv("REAPER_BATCH_SIZE", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Reaper.Interval != 10*time.Second || cfg.Reaper.MaxAttempts != 3 ||
		cfg.Reaper.StreamMinIdle != 2*time.Minute || cfg.Reaper.BatchSize != 32 {
		t.Fatalf("Reaper defaults = %#v", cfg.Reaper)
	}
	if cfg.Reaper.StreamMinIdle <= cfg.Judge.AttemptLease {
		t.Fatalf("stream min idle %v does not exceed attempt lease %v", cfg.Reaper.StreamMinIdle, cfg.Judge.AttemptLease)
	}
}

func TestLoadRejectsShortReaperStreamMinIdle(t *testing.T) {
	t.Setenv("JUDGE_ATTEMPT_LEASE", "1m")
	t.Setenv("REAPER_STREAM_MIN_IDLE", "1m")
	if _, err := Load(); err == nil {
		t.Fatal("Load returned nil error for stream min idle equal to attempt lease")
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

func TestLoadAuthDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("AUTH_TOKEN_TTL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AppEnv != "development" {
		t.Fatalf("AppEnv = %q, want development", cfg.AppEnv)
	}
	if cfg.Auth.TokenTTL != 24*time.Hour {
		t.Fatalf("TokenTTL = %v, want 24h", cfg.Auth.TokenTTL)
	}
}

func TestLoadRejectsUnknownAppEnv(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted unknown APP_ENV")
	}
}

func TestLoadAcceptsTestAndProductionEnv(t *testing.T) {
	for _, env := range []string{"test", "production"} {
		t.Setenv("APP_ENV", env)
		if _, err := Load(); err != nil {
			t.Fatalf("Load APP_ENV=%s: %v", env, err)
		}
	}
}

func TestLoadRejectsInvalidTokenTTL(t *testing.T) {
	t.Setenv("AUTH_TOKEN_TTL", "0s")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted non-positive AUTH_TOKEN_TTL")
	}
}

func TestLoadAuthTokenTTLOverride(t *testing.T) {
	t.Setenv("AUTH_TOKEN_TTL", "1h")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.TokenTTL != time.Hour {
		t.Fatalf("TokenTTL = %v, want 1h", cfg.Auth.TokenTTL)
	}
}

func TestValidateForRoleProductionGatewayRejectsWeakSecrets(t *testing.T) {
	strong := "0123456789abcdef0123456789abcdef"
	tests := []struct {
		name   string
		role   string
		appEnv string
		secret string
	}{
		{"default secret", "gateway", "production", "codeduel-dev-secret"},
		{"short secret", "gateway", "production", "short"},
		{"whitespace padded", "gateway", "production", "  " + strong + "  "},
		{"development allows default", "gateway", "development", "codeduel-dev-secret"},
		{"match role skips validation", "match", "production", "codeduel-dev-secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{AppEnv: tt.appEnv, Gateway: GatewayConfig{JWTSecret: tt.secret}}
			if tt.name == "development allows default" || tt.name == "match role skips validation" {
				if err := cfg.ValidateForRole(tt.role); err != nil {
					t.Fatalf("ValidateForRole: %v", err)
				}
				return
			}
			if err := cfg.ValidateForRole(tt.role); err == nil {
				t.Fatal("ValidateForRole accepted an unsafe production gateway secret")
			}
		})
	}
}

func TestValidateForRoleProductionGatewayAcceptsStrongSecret(t *testing.T) {
	cfg := &Config{AppEnv: "production", Gateway: GatewayConfig{JWTSecret: "0123456789abcdef0123456789abcdef"}}
	if err := cfg.ValidateForRole("gateway"); err != nil {
		t.Fatalf("ValidateForRole: %v", err)
	}
}
