package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	authpkg "github.com/nglong14/CodeDuel/internal/auth"
)

type fakeAccounts struct {
	registerFn func(context.Context, string, string) (authpkg.User, error)
	loginFn    func(context.Context, string, string) (authpkg.User, error)
	byIDFn     func(context.Context, uuid.UUID) (authpkg.User, error)
}

func (f *fakeAccounts) Register(ctx context.Context, email, password string) (authpkg.User, error) {
	if f.registerFn == nil {
		return authpkg.User{}, errors.New("unexpected Register call")
	}
	return f.registerFn(ctx, email, password)
}

func (f *fakeAccounts) Login(ctx context.Context, email, password string) (authpkg.User, error) {
	if f.loginFn == nil {
		return authpkg.User{}, errors.New("unexpected Login call")
	}
	return f.loginFn(ctx, email, password)
}

func (f *fakeAccounts) UserByID(ctx context.Context, id uuid.UUID) (authpkg.User, error) {
	if f.byIDFn == nil {
		return authpkg.User{}, errors.New("unexpected UserByID call")
	}
	return f.byIDFn(ctx, id)
}

func testAuthHTTP(accounts accountService) *authHTTP {
	return &authHTTP{
		accounts: accounts,
		secret:   testSecret,
		tokenTTL: time.Hour,
		logger:   slog.New(slog.DiscardHandler),
	}
}

func sessionRequest(t *testing.T, path, body string, headers map[string]string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if headers == nil {
		headers = map[string]string{"Content-Type": "application/json"}
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return req
}

func TestLoginSuccess(t *testing.T) {
	accounts := &fakeAccounts{
		loginFn: func(_ context.Context, email, password string) (authpkg.User, error) {
			if email != "alice@example.com" || password != "password1" {
				t.Fatalf("Login(%q, %q)", email, password)
			}
			return testUser(), nil
		},
	}
	rec := httptest.NewRecorder()
	testAuthHTTP(accounts).login(rec, sessionRequest(t, "/api/auth/login", `{"email":"  Alice@Example.COM ","password":"password1"}`, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("CORS header emitted: %q", got)
	}
	var session sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session.Token == "" {
		t.Fatal("empty token")
	}
	userID, exp, err := parseAccessToken(session.Token, testSecret)
	if err != nil {
		t.Fatalf("parse returned token: %v", err)
	}
	if userID != testUserID() || session.User.ID != testUserID() {
		t.Fatalf("token subject = %s, body user = %s", userID, session.User.ID)
	}
	if session.ExpiresAt.IsZero() || exp.IsZero() {
		t.Fatalf("expires_at = %v, parsed exp = %v", session.ExpiresAt, exp)
	}
	if diff := session.ExpiresAt.Sub(exp); diff < -time.Second || diff > time.Second {
		t.Fatalf("expires_at = %v, parsed exp = %v differ by %v", session.ExpiresAt, exp, diff)
	}
	if body := rec.Body.String(); strings.Contains(body, "password") {
		t.Fatalf("response leaks password material: %s", body)
	}
}

func TestRegisterSuccess(t *testing.T) {
	accounts := &fakeAccounts{
		registerFn: func(_ context.Context, email, password string) (authpkg.User, error) {
			if email != "bob@example.com" {
				t.Fatalf("email = %q", email)
			}
			return authpkg.User{
				ID:          uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				Email:       email,
				DisplayName: "bob",
			}, nil
		},
	}
	rec := httptest.NewRecorder()
	req := sessionRequest(t, "/api/auth/register", `{"email":" bob@example.com ","password":"password1"}`, map[string]string{"Content-Type": "application/json; charset=utf-8"})
	testAuthHTTP(accounts).register(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	var session sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	if session.User.Email != "bob@example.com" || session.Token == "" {
		t.Fatalf("session = %#v", session)
	}
}

func TestSessionErrorMappings(t *testing.T) {
	tests := []struct {
		name       string
		register   bool
		accountsFn func() *fakeAccounts
		wantStatus int
		wantCode   string
	}{
		{"login invalid credentials", false, func() *fakeAccounts {
			return &fakeAccounts{loginFn: func(context.Context, string, string) (authpkg.User, error) {
				return authpkg.User{}, authpkg.ErrInvalidCredentials
			}}
		}, http.StatusUnauthorized, "invalid_credentials"},
		{"register email taken", true, func() *fakeAccounts {
			return &fakeAccounts{registerFn: func(context.Context, string, string) (authpkg.User, error) {
				return authpkg.User{}, authpkg.ErrEmailTaken
			}}
		}, http.StatusConflict, "email_taken"},
		{"service internal error", false, func() *fakeAccounts {
			return &fakeAccounts{loginFn: func(context.Context, string, string) (authpkg.User, error) {
				return authpkg.User{}, errors.New("database unavailable")
			}}
		}, http.StatusInternalServerError, "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := testAuthHTTP(tt.accountsFn())
			rec := httptest.NewRecorder()
			body := `{"email":"alice@example.com","password":"password1"}`
			if tt.register {
				h.register(rec, sessionRequest(t, "/api/auth/register", body, nil))
			} else {
				h.login(rec, sessionRequest(t, "/api/auth/login", body, nil))
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var envelope errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if envelope.Error.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", envelope.Error.Code, tt.wantCode)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
			if body := rec.Body.String(); strings.Contains(body, `"password1"`) || strings.Contains(body, "password_hash") || strings.Contains(body, "bcrypt") {
				t.Fatalf("error response leaks sensitive material: %s", body)
			}
		})
	}
}

func TestSessionRejectsMalformedRequests(t *testing.T) {
	h := testAuthHTTP(&fakeAccounts{loginFn: func(context.Context, string, string) (authpkg.User, error) {
		t.Fatal("service invoked for malformed request")
		return authpkg.User{}, nil
	}})
	oversized := `{"email":"alice@example.com","password":"` + strings.Repeat("p", 5000) + `"}`
	tests := []struct {
		name       string
		headers    map[string]string
		body       string
		wantStatus int
	}{
		{"missing content type", nil, `{"email":"a@b.com","password":"password1"}`, http.StatusUnsupportedMediaType},
		{"wrong content type", map[string]string{"Content-Type": "text/plain"}, `{"email":"a@b.com","password":"password1"}`, http.StatusUnsupportedMediaType},
		{"empty body", map[string]string{"Content-Type": "application/json"}, ``, http.StatusBadRequest},
		{"malformed json", map[string]string{"Content-Type": "application/json"}, `{`, http.StatusBadRequest},
		{"unknown field", map[string]string{"Content-Type": "application/json"}, `{"email":"a@b.com","password":"password1","admin":true}`, http.StatusBadRequest},
		{"trailing data", map[string]string{"Content-Type": "application/json"}, `{"email":"a@b.com","password":"password1"} {}`, http.StatusBadRequest},
		{"oversized body", map[string]string{"Content-Type": "application/json"}, oversized, http.StatusRequestEntityTooLarge},
		{"invalid email", map[string]string{"Content-Type": "application/json"}, `{"email":"not-an-email","password":"password1"}`, http.StatusBadRequest},
		{"short password", map[string]string{"Content-Type": "application/json"}, `{"email":"a@b.com","password":"short"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := tt.headers
			if headers == nil {
				headers = map[string]string{}
			}
			rec := httptest.NewRecorder()
			h.login(rec, sessionRequest(t, "/api/auth/login", tt.body, headers))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			var envelope errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if envelope.Error.Code == "" {
				t.Fatal("missing error code")
			}
		})
	}
}

func TestMeAuthenticated(t *testing.T) {
	accounts := &fakeAccounts{byIDFn: func(context.Context, uuid.UUID) (authpkg.User, error) {
		return testUser(), nil
	}}
	raw, err := MintToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	testAuthHTTP(accounts).me(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var me meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.User.ID != testUserID() || me.User.Email != "alice@example.com" {
		t.Fatalf("me = %#v", me.User)
	}
}

func TestMeRejectsQueryTokenAndMissingCredentials(t *testing.T) {
	accounts := &fakeAccounts{byIDFn: func(context.Context, uuid.UUID) (authpkg.User, error) {
		return testUser(), nil
	}}
	raw, err := MintToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	h := testAuthHTTP(accounts)
	tests := []struct {
		name string
		req  *http.Request
	}{
		{"no credentials", httptest.NewRequest(http.MethodGet, "/api/me", nil)},
		{"query token only", httptest.NewRequest(http.MethodGet, "/api/me?token="+raw, nil)},
		{"malformed bearer", bearerRequest("Bearer not-a-jwt")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.me(rec, tt.req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Fatalf("WWW-Authenticate = %q, want Bearer", got)
			}
			var envelope errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error: %v", err)
			}
			if envelope.Error.Code != "unauthorized" {
				t.Fatalf("code = %q, want unauthorized", envelope.Error.Code)
			}
		})
	}
}

func TestWriteJSONAndErrorAreWellFormed(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, meResponse{User: testUser()})
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &meResponse{}); err != nil {
		t.Fatalf("decode writeJSON: %v", err)
	}
	if _, err := io.ReadAll(rec.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
}
