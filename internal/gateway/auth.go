package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	accountauth "github.com/nglong14/CodeDuel/internal/auth"
)

type IssuedToken struct {
	Value     string
	ExpiresAt time.Time
}

type Principal struct {
	User      accountauth.User
	ExpiresAt time.Time
}

type UserLookup interface {
	UserByID(context.Context, uuid.UUID) (accountauth.User, error)
}

func MintAccessToken(userID uuid.UUID, secret string, ttl time.Duration) (IssuedToken, error) {
	if secret == "" {
		return IssuedToken{}, fmt.Errorf("mint token: empty secret")
	}
	if userID == uuid.Nil {
		return IssuedToken{}, fmt.Errorf("mint token: empty user ID")
	}
	if ttl <= 0 {
		return IssuedToken{}, fmt.Errorf("mint token: non-positive TTL")
	}
	now := time.Now()
	expiresAt := now.Add(ttl)
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return IssuedToken{}, fmt.Errorf("mint token: %w", err)
	}
	return IssuedToken{Value: signed, ExpiresAt: expiresAt}, nil
}

// MintToken remains the development CLI's compact token-only API.
func MintToken(userID uuid.UUID, secret string, ttl time.Duration) (string, error) {
	issued, err := MintAccessToken(userID, secret, ttl)
	if err != nil {
		return "", err
	}
	return issued.Value, nil
}

func AuthenticateREST(ctx context.Context, r *http.Request, secret string, users UserLookup) (Principal, error) {
	raw, err := extractBearerToken(r)
	if err != nil {
		return Principal{}, err
	}
	return authenticateToken(ctx, raw, secret, users)
}

func AuthenticateWebSocket(ctx context.Context, r *http.Request, secret string, users UserLookup) (Principal, error) {
	raw, err := extractWebSocketToken(r)
	if err != nil {
		return Principal{}, err
	}
	return authenticateToken(ctx, raw, secret, users)
}

// Authenticate preserves the original WebSocket-oriented API for existing callers.
func Authenticate(ctx context.Context, r *http.Request, secret string, db *pgxpool.Pool) (uuid.UUID, error) {
	principal, err := AuthenticateWebSocket(ctx, r, secret, accountauth.NewService(db))
	if err != nil {
		return uuid.Nil, err
	}
	return principal.User.ID, nil
}

func authenticateToken(ctx context.Context, raw, secret string, users UserLookup) (Principal, error) {
	userID, expiresAt, err := parseAccessToken(raw, secret)
	if err != nil {
		return Principal{}, err
	}
	if users == nil {
		return Principal{}, fmt.Errorf("authenticate token: missing user lookup")
	}
	user, err := users.UserByID(ctx, userID)
	if err != nil {
		return Principal{}, fmt.Errorf("authenticate token: lookup user: %w", err)
	}
	return Principal{User: user, ExpiresAt: expiresAt}, nil
}

func extractBearerToken(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", fmt.Errorf("missing or repeated Authorization header")
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t") {
		return "", fmt.Errorf("malformed Bearer token")
	}
	return token, nil
}

func extractWebSocketToken(r *http.Request) (string, error) {
	if len(r.Header.Values("Authorization")) > 0 {
		return extractBearerToken(r)
	}
	values, ok := r.URL.Query()["token"]
	if !ok || len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return "", fmt.Errorf("missing or repeated token")
	}
	return values[0], nil
}

func extractToken(r *http.Request) (string, error) {
	return extractWebSocketToken(r)
}

func parseAccessToken(raw, secret string) (uuid.UUID, time.Time, error) {
	if secret == "" {
		return uuid.Nil, time.Time{}, fmt.Errorf("empty secret")
	}
	token, err := jwt.Parse(raw, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("parse token: %w", err)
	}
	subject, err := token.Claims.GetSubject()
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("subject: %w", err)
	}
	userID, err := uuid.Parse(subject)
	if err != nil || userID == uuid.Nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("subject: invalid user ID")
	}
	expiration, err := token.Claims.GetExpirationTime()
	if err != nil || expiration == nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("expiration: missing")
	}
	return userID, expiration.Time, nil
}

func parseToken(raw, secret string) (uuid.UUID, error) {
	userID, _, err := parseAccessToken(raw, secret)
	return userID, err
}
