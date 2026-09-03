package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nglong14/CodeDuel/internal/app"
	"github.com/nglong14/CodeDuel/internal/config"
)

func testGatewayDeps() *app.Dependencies {
	return &app.Dependencies{
		Config: &config.Config{
			Gateway: config.GatewayConfig{
				Addr:      "127.0.0.1:0",
				JWTSecret: testSecret,
			},
		},
		Logger: slog.New(slog.DiscardHandler),
	}
}

func TestHealthz(t *testing.T) {
	h := newHandler(context.Background(), testGatewayDeps(), NewRegistry())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyzUnavailableWithoutPostgres(t *testing.T) {
	h := newHandler(context.Background(), testGatewayDeps(), NewRegistry())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestMeRejectsQueryTokenThroughMux(t *testing.T) {
	h := newHandler(context.Background(), testGatewayDeps(), NewRegistry())
	raw, err := MintToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/me?token="+raw, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthLoginRequiresJSONContentType(t *testing.T) {
	h := newHandler(context.Background(), testGatewayDeps(), NewRegistry())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"a@b.com","password":"password1"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", rec.Code)
	}
}

func TestWSUnauthorized(t *testing.T) {
	h := newHandler(context.Background(), testGatewayDeps(), NewRegistry())

	tests := []struct {
		name string
		req  *http.Request
	}{
		{"missing token", httptest.NewRequest(http.MethodGet, "/ws", nil)},
		{"raw uuid", httptest.NewRequest(http.MethodGet, "/ws?token=11111111-1111-1111-1111-111111111111", nil)},
		{"bad bearer", bearerRequest("Bearer not-a-jwt")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tt.req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != "unauthorized" {
				t.Fatalf("body = %q, want unauthorized", got)
			}
		})
	}
}
