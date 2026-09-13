package infrastructure

import (
	"strings"
	"testing"
)

func TestRedisOptionsURLTakesPrecedenceOverAddr(t *testing.T) {
	opts, err := RedisOptions("redis://managed.example.com:6380", "localhost:6379")
	if err != nil {
		t.Fatalf("RedisOptions: %v", err)
	}
	if opts.Addr != "managed.example.com:6380" {
		t.Fatalf("Addr = %q, want managed.example.com:6380", opts.Addr)
	}
	if !opts.ContextTimeoutEnabled {
		t.Fatal("ContextTimeoutEnabled = false, want true")
	}
}

func TestRedisOptionsFallsBackToAddr(t *testing.T) {
	opts, err := RedisOptions("", "localhost:6379")
	if err != nil {
		t.Fatalf("RedisOptions: %v", err)
	}
	if opts.Addr != "localhost:6379" {
		t.Fatalf("Addr = %q, want localhost:6379", opts.Addr)
	}
	if opts.TLSConfig != nil {
		t.Fatal("TLSConfig set for plain addr fallback")
	}
	if !opts.ContextTimeoutEnabled {
		t.Fatal("ContextTimeoutEnabled = false, want true")
	}
}

func TestRedisOptionsParsesAuthentication(t *testing.T) {
	opts, err := RedisOptions("redis://alice:s3cret@managed.example.com:6379", "localhost:6379")
	if err != nil {
		t.Fatalf("RedisOptions: %v", err)
	}
	if opts.Username != "alice" || opts.Password != "s3cret" {
		t.Fatalf("credentials = %q/%q, want alice/s3cret", opts.Username, opts.Password)
	}
}

func TestRedisOptionsSelectsDatabase(t *testing.T) {
	opts, err := RedisOptions("redis://managed.example.com:6379/7", "localhost:6379")
	if err != nil {
		t.Fatalf("RedisOptions: %v", err)
	}
	if opts.DB != 7 {
		t.Fatalf("DB = %d, want 7", opts.DB)
	}
}

func TestRedisOptionsEnablesTLSForRediss(t *testing.T) {
	opts, err := RedisOptions("rediss://managed.example.com:6379", "localhost:6379")
	if err != nil {
		t.Fatalf("RedisOptions: %v", err)
	}
	if opts.TLSConfig == nil {
		t.Fatal("TLSConfig = nil, want TLS enabled for rediss://")
	}
	// A nil RootCAs pool means Go verifies against the system CA certificates.
	if opts.TLSConfig.RootCAs != nil {
		t.Fatal("RootCAs set, want system CA pool (nil)")
	}
	if opts.TLSConfig.ServerName != "managed.example.com" {
		t.Fatalf("ServerName = %q, want managed.example.com", opts.TLSConfig.ServerName)
	}
}

func TestRedisOptionsRejectsMalformedURLWithoutLeakingCredentials(t *testing.T) {
	const secret = "sup3rsecret"
	// A non-numeric database segment makes redis.ParseURL fail after the credentials
	// have been parsed, exercising the sanitized error path.
	_, err := RedisOptions("redis://user:"+secret+"@managed.example.com:6379/not-a-number", "localhost:6379")
	if err == nil {
		t.Fatal("RedisOptions accepted a malformed URL")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked credentials: %v", err)
	}
}

func TestRedisOptionsRejectsUnknownScheme(t *testing.T) {
	if _, err := RedisOptions("http://managed.example.com:6379", "localhost:6379"); err == nil {
		t.Fatal("RedisOptions accepted a non-redis scheme")
	}
}

func TestRedisHostReturnsCredentialFreeEndpoint(t *testing.T) {
	got := RedisHost("rediss://user:sup3rsecret@managed.example.com:6379/2", "localhost:6379")
	if got != "managed.example.com:6379" {
		t.Fatalf("RedisHost = %q, want managed.example.com:6379", got)
	}
	if strings.Contains(got, "sup3rsecret") || strings.Contains(got, "user") {
		t.Fatalf("RedisHost leaked credentials: %q", got)
	}
}

func TestRedisHostFallsBackToAddr(t *testing.T) {
	if got := RedisHost("", "localhost:6379"); got != "localhost:6379" {
		t.Fatalf("RedisHost = %q, want localhost:6379", got)
	}
}

func TestRedisHostReturnsEmptyForMalformedURL(t *testing.T) {
	if got := RedisHost("redis://user:pw@host:6379/not-a-number", "localhost:6379"); got != "" {
		t.Fatalf("RedisHost = %q, want empty string for malformed URL", got)
	}
}
