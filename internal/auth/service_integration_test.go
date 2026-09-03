package auth

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/nglong14/CodeDuel/internal/infrastructure"
)

func TestAuthServicePostgresIntegration(t *testing.T) {
	if os.Getenv("CODEDUEL_INTEGRATION") != "1" {
		t.Skip("set CODEDUEL_INTEGRATION=1 to run integration tests")
	}
	ctx := context.Background()
	baseDSN := os.Getenv("POSTGRES_TEST_DSN")
	if baseDSN == "" {
		baseDSN = "postgres://codeduel:codeduel@localhost:5433/postgres?sslmode=disable"
	}
	admin, err := pgxpool.New(ctx, baseDSN)
	if err != nil {
		t.Fatalf("open admin PostgreSQL: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Fatalf("connect to integration PostgreSQL: %v", err)
	}
	database := "codeduel_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+database); err != nil {
		admin.Close()
		t.Fatalf("create integration database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+database+" WITH (FORCE)")
		admin.Close()
	})
	testURL, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse POSTGRES_TEST_DSN: %v", err)
	}
	testURL.Path = "/" + database
	testDSN := testURL.String()
	if err := infrastructure.MigrateUp(ctx, testDSN); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := pgxpool.New(ctx, testDSN)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()
	svc := NewService(pool)

	registered, err := svc.Register(ctx, "  Carol@Example.COM ", "carol-password")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if registered.Email != "carol@example.com" || registered.DisplayName != "carol" {
		t.Fatalf("registered user = %#v", registered)
	}
	var storedHash string
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, registered.ID).Scan(&storedHash); err != nil {
		t.Fatalf("query stored hash: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(storedHash), []byte("carol-password")) != nil {
		t.Fatal("stored hash does not verify plaintext")
	}

	loggedIn, err := svc.Login(ctx, "CAROL@example.com", "carol-password")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if loggedIn.ID != registered.ID {
		t.Fatalf("login user ID = %s, want %s", loggedIn.ID, registered.ID)
	}

	if _, err := svc.Login(ctx, "carol@example.com", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login wrong password = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(ctx, "unknown@example.com", "some-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login unknown account = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(ctx, "alice@codeduel.dev", "whatever-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login seeded CLI-only account = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Register(ctx, "carol@example.com", "another-password"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate Register = %v, want ErrEmailTaken", err)
	}

	byID, err := svc.UserByID(ctx, registered.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if byID.Email != "carol@example.com" {
		t.Fatalf("UserByID email = %q", byID.Email)
	}
}
