package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func MintToken(userID uuid.UUID, secret string, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("mint token: empty secret")
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("mint token: %w", err)
	}
	return signed, nil
}

func Authenticate(ctx context.Context, r *http.Request, secret string, db *pgxpool.Pool) (uuid.UUID, error) {
	raw, err := extractToken(r)
	if err != nil {
		return uuid.Nil, err
	}
	userID, err := parseToken(raw, secret)
	if err != nil {
		return uuid.Nil, err
	}

	var id uuid.UUID
	err = db.QueryRow(ctx, `SELECT id FROM users WHERE id = $1`, userID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("user not found")
		}
		return uuid.Nil, fmt.Errorf("lookup user: %w", err)
	}
	return id, nil
}

func extractToken(r *http.Request) (string, error) {
	if auth := r.Header.Get("Authorization"); auth != "" {
		scheme, token, ok := strings.Cut(auth, " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			token = strings.TrimSpace(token)
			if token == "" {
				return "", fmt.Errorf("missing token")
			}
			return token, nil
		}
	}
	if token := r.URL.Query().Get("token"); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("missing token")
}

func parseToken(raw, secret string) (uuid.UUID, error) {
	if secret == "" {
		return uuid.Nil, fmt.Errorf("empty secret")
	}
	token, err := jwt.Parse(raw, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse token: %w", err)
	}

	sub, err := token.Claims.GetSubject()
	if err != nil {
		return uuid.Nil, fmt.Errorf("subject: %w", err)
	}
	userID, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil, fmt.Errorf("subject: %w", err)
	}
	return userID, nil
}
