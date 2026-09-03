package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	authpkg "github.com/nglong14/CodeDuel/internal/auth"
)

const testSecret = "test-jwt-secret"

func testUserID() uuid.UUID {
	return uuid.MustParse("11111111-1111-1111-1111-111111111111")
}

func TestMintTokenRoundTrip(t *testing.T) {
	userID := testUserID()
	raw, err := MintToken(userID, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	got, err := parseToken(raw, testSecret)
	if err != nil {
		t.Fatalf("parseToken: %v", err)
	}
	if got != userID {
		t.Fatalf("got %s, want %s", got, userID)
	}
}

func TestMintTokenEmptySecret(t *testing.T) {
	if _, err := MintToken(testUserID(), "", time.Hour); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthenticateExtractsBearerHeader(t *testing.T) {
	raw, err := MintToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws?token=ignored", nil)
	req.Header.Set("Authorization", "Bearer "+raw)

	got, err := extractToken(req)
	if err != nil {
		t.Fatalf("extractToken: %v", err)
	}
	if got != raw {
		t.Fatalf("used query token instead of Bearer header")
	}
}

func TestAuthenticateFallsBackToQueryToken(t *testing.T) {
	raw, err := MintToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws?token="+raw, nil)
	got, err := extractToken(req)
	if err != nil {
		t.Fatalf("extractToken: %v", err)
	}
	if got != raw {
		t.Fatalf("got %q, want minted token", got)
	}
}

func TestAuthenticateRejects(t *testing.T) {
	ctx := context.Background()
	userID := testUserID()
	valid, err := MintToken(userID, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	expiredClaims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
	}
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, expiredClaims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign expired: %v", err)
	}

	noExpClaims := jwt.RegisteredClaims{
		Subject:  userID.String(),
		IssuedAt: jwt.NewNumericDate(time.Now()),
	}
	noExp, err := jwt.NewWithClaims(jwt.SigningMethodHS256, noExpClaims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign no exp: %v", err)
	}

	noneClaims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	noneToken := jwt.NewWithClaims(jwt.SigningMethodNone, noneClaims)
	none, err := noneToken.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}

	hs384Claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	hs384, err := jwt.NewWithClaims(jwt.SigningMethodHS384, hs384Claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign hs384: %v", err)
	}

	badSubClaims := jwt.RegisteredClaims{
		Subject:   "not-a-uuid",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	badSub, err := jwt.NewWithClaims(jwt.SigningMethodHS256, badSubClaims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign bad sub: %v", err)
	}

	noSubClaims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	noSub, err := jwt.NewWithClaims(jwt.SigningMethodHS256, noSubClaims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign no sub: %v", err)
	}

	wrongSecret, err := MintToken(userID, "other-secret", time.Hour)
	if err != nil {
		t.Fatalf("MintToken wrong secret: %v", err)
	}

	tests := []struct {
		name string
		req  *http.Request
	}{
		{"missing token", httptest.NewRequest(http.MethodGet, "/ws", nil)},
		{"empty bearer", bearerRequest("Bearer ")},
		{"bad signature", bearerRequest("Bearer " + wrongSecret)},
		{"expired", bearerRequest("Bearer " + expired)},
		{"missing exp", bearerRequest("Bearer " + noExp)},
		{"alg none", bearerRequest("Bearer " + none)},
		{"non-HS256", bearerRequest("Bearer " + hs384)},
		{"sub not uuid", bearerRequest("Bearer " + badSub)},
		{"missing sub", bearerRequest("Bearer " + noSub)},
		{"raw uuid is not a jwt", httptest.NewRequest(http.MethodGet, "/ws?token="+userID.String(), nil)},
		{"valid token wrong secret", func() *http.Request {
			req := bearerRequest("Bearer " + valid)
			return req
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := testSecret
			if tt.name == "valid token wrong secret" {
				secret = "not-the-signing-secret"
			}
			if _, err := Authenticate(ctx, tt.req, secret, nil); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func bearerRequest(authorization string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Authorization", authorization)
	return req
}

type stubUserLookup struct {
	user authpkg.User
	err  error
}

func (s stubUserLookup) UserByID(context.Context, uuid.UUID) (authpkg.User, error) {
	return s.user, s.err
}

func testUser() authpkg.User {
	return authpkg.User{
		ID:          testUserID(),
		Email:       "alice@example.com",
		DisplayName: "alice",
	}
}

func TestParseAccessTokenReturnsExpiration(t *testing.T) {
	issued, err := MintAccessToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintAccessToken: %v", err)
	}
	gotID, gotExp, err := parseAccessToken(issued.Value, testSecret)
	if err != nil {
		t.Fatalf("parseAccessToken: %v", err)
	}
	if gotID != testUserID() {
		t.Fatalf("subject = %s, want %s", gotID, testUserID())
	}
	if gotExp.Before(time.Now().Add(59*time.Minute)) || gotExp.After(time.Now().Add(61*time.Minute)) {
		t.Fatalf("expiration = %v, want about one hour from now", gotExp)
	}
}

func TestMintAccessTokenRejectsInvalidInput(t *testing.T) {
	if _, err := MintAccessToken(testUserID(), "", time.Hour); err == nil {
		t.Fatal("empty secret accepted")
	}
	if _, err := MintAccessToken(uuid.Nil, testSecret, time.Hour); err == nil {
		t.Fatal("empty user ID accepted")
	}
	if _, err := MintAccessToken(testUserID(), testSecret, 0); err == nil {
		t.Fatal("non-positive TTL accepted")
	}
}

func TestAuthenticateRESTRejectsQueryToken(t *testing.T) {
	raw, err := MintToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/me?token="+raw, nil)
	if _, err := AuthenticateREST(context.Background(), req, testSecret, stubUserLookup{user: testUser()}); err == nil {
		t.Fatal("AuthenticateREST accepted a query token")
	}
}

func TestAuthenticateRESTAcceptsBearerToken(t *testing.T) {
	raw, err := MintToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	principal, err := AuthenticateREST(context.Background(), req, testSecret, stubUserLookup{user: testUser()})
	if err != nil {
		t.Fatalf("AuthenticateREST: %v", err)
	}
	if principal.User.ID != testUserID() {
		t.Fatalf("principal user = %s, want %s", principal.User.ID, testUserID())
	}
}

func TestAuthenticateWebSocketQueryToken(t *testing.T) {
	raw, err := MintToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/ws?token="+raw, nil)
	principal, err := AuthenticateWebSocket(context.Background(), req, testSecret, stubUserLookup{user: testUser()})
	if err != nil {
		t.Fatalf("AuthenticateWebSocket: %v", err)
	}
	if principal.User.ID != testUserID() {
		t.Fatalf("principal user = %s, want %s", principal.User.ID, testUserID())
	}
}

func TestAuthenticateWebSocketBearerTakesPrecedence(t *testing.T) {
	headerToken, err := MintToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	otherID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	queryToken, err := MintToken(otherID, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/ws?token="+queryToken, nil)
	req.Header.Set("Authorization", "Bearer "+headerToken)
	principal, err := AuthenticateWebSocket(context.Background(), req, testSecret, stubUserLookup{user: testUser()})
	if err != nil {
		t.Fatalf("AuthenticateWebSocket: %v", err)
	}
	if principal.User.ID != testUserID() {
		t.Fatalf("used query token instead of Bearer header")
	}
}

func TestAuthenticateRejectsAmbiguousCredentials(t *testing.T) {
	valid, err := MintToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	repeatedHeaders := httptest.NewRequest(http.MethodGet, "/ws", nil)
	repeatedHeaders.Header.Add("Authorization", "Bearer "+valid)
	repeatedHeaders.Header.Add("Authorization", "Bearer "+valid)
	repeatedQuery := httptest.NewRequest(http.MethodGet, "/ws?token="+valid+"&token="+valid, nil)

	malformedWithQuery := httptest.NewRequest(http.MethodGet, "/ws?token="+valid, nil)
	malformedWithQuery.Header.Set("Authorization", "Basic abc")
	malformedSpaces := bearerRequest("Bearer  " + valid)

	tests := []struct {
		name string
		req  *http.Request
	}{
		{"repeated authorization headers", repeatedHeaders},
		{"repeated query tokens", repeatedQuery},
		{"malformed header no query fallback", malformedWithQuery},
		{"bearer with embedded spaces", malformedSpaces},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := AuthenticateWebSocket(context.Background(), tt.req, testSecret, stubUserLookup{user: testUser()}); err == nil {
				t.Fatal("AuthenticateWebSocket accepted ambiguous credentials")
			}
		})
	}
}

func TestAuthenticateRejectsUnknownUser(t *testing.T) {
	raw, err := MintToken(testUserID(), testSecret, time.Hour)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	lookup := stubUserLookup{err: authpkg.ErrUserNotFound}
	if _, err := AuthenticateWebSocket(context.Background(), req, testSecret, lookup); err == nil {
		t.Fatal("AuthenticateWebSocket accepted an unknown user")
	}
}
