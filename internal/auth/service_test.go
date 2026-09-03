package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

type fakeRow struct {
	err    error
	values []any
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fmt.Errorf("scan: %d destinations for %d values", len(dest), len(r.values))
	}
	for i := range dest {
		dv := reflect.ValueOf(dest[i])
		if dv.Kind() != reflect.Pointer {
			return fmt.Errorf("scan: destination %d is not a pointer", i)
		}
		elem := dv.Elem()
		value := r.values[i]
		if value == nil {
			elem.Set(reflect.Zero(elem.Type()))
			continue
		}
		rv := reflect.ValueOf(value)
		switch {
		case rv.Type().AssignableTo(elem.Type()):
			elem.Set(rv)
		case elem.Kind() == reflect.Pointer && rv.Type().AssignableTo(elem.Type().Elem()):
			allocated := reflect.New(elem.Type().Elem())
			allocated.Elem().Set(rv)
			elem.Set(allocated)
		default:
			return fmt.Errorf("scan: cannot assign %T to %s", value, elem.Type())
		}
	}
	return nil
}

type fakeDB struct {
	query func(sql string, args ...any) pgx.Row
}

func (d *fakeDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	return d.query(sql, args...)
}

func noRows() pgx.Row { return fakeRow{err: pgx.ErrNoRows} }

func TestNormalizeEmail(t *testing.T) {
	valid := []struct {
		in   string
		want string
	}{
		{"  Alice@Example.COM ", "alice@example.com"},
		{"alice@example.com", "alice@example.com"},
		{" Alice.Bob+tag@Example.co.uk ", "alice.bob+tag@example.co.uk"},
	}
	for _, tt := range valid {
		got, err := NormalizeEmail(tt.in)
		if err != nil {
			t.Fatalf("NormalizeEmail(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("NormalizeEmail(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	invalid := []string{
		"",
		"   ",
		"not-an-email",
		"a@",
		"@b.com",
		"a@b@c.com",
		"user @example.com",
		strings.Repeat("a", 65) + "@example.com",
		"user@" + strings.Repeat("b", 250) + ".com",
		strings.Repeat("a", 3) + "@" + strings.Repeat("b", 250) + ".com",
	}
	for _, in := range invalid {
		if _, err := NormalizeEmail(in); err == nil {
			t.Fatalf("NormalizeEmail(%q) accepted invalid email", in)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("a", 8)); err != nil {
		t.Fatalf("8-byte password rejected: %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", 72)); err != nil {
		t.Fatalf("72-byte password rejected: %v", err)
	}
	if err := ValidatePassword(strings.Repeat("a", 7)); err == nil {
		t.Fatal("7-byte password accepted")
	}
	if err := ValidatePassword(strings.Repeat("a", 73)); err == nil {
		t.Fatal("73-byte password accepted")
	}
	if err := ValidatePassword(strings.Repeat("é", 4)); err != nil {
		t.Fatalf("8-byte multibyte password rejected: %v", err)
	}
	if err := ValidatePassword(string([]byte{0xff, 0xfe})); err == nil {
		t.Fatal("non-UTF-8 password accepted")
	}
}

func userFixture() (uuid.UUID, time.Time) {
	return uuid.MustParse("11111111-1111-1111-1111-111111111111"), time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
}

func TestRegisterNormalizesAndDerivesDisplayName(t *testing.T) {
	id, createdAt := userFixture()
	var gotPassword string
	db := &fakeDB{query: func(sql string, args ...any) pgx.Row {
		gotPassword, _ = args[1].(string)
		return fakeRow{values: []any{id, args[0], args[2], createdAt}}
	}}
	svc := NewService(db)
	user, err := svc.Register(context.Background(), "  Alice@Example.COM ", "password1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if user.Email != "alice@example.com" || user.DisplayName != "alice" || user.ID != id {
		t.Fatalf("registered user = %#v", user)
	}
	if bcrypt.CompareHashAndPassword([]byte(gotPassword), []byte("password1")) != nil {
		t.Fatal("stored hash does not verify the plaintext password")
	}
}

func TestRegisterMapsEmailTakenConstraint(t *testing.T) {
	db := &fakeDB{query: func(string, ...any) pgx.Row {
		return fakeRow{err: &pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"}}
	}}
	_, err := NewService(db).Register(context.Background(), "alice@example.com", "password1")
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("Register = %v, want ErrEmailTaken", err)
	}
}

func TestRegisterDoesNotMapUnknownUniqueViolation(t *testing.T) {
	db := &fakeDB{query: func(string, ...any) pgx.Row {
		return fakeRow{err: &pgconn.PgError{Code: "23505", ConstraintName: "users_pkey"}}
	}}
	_, err := NewService(db).Register(context.Background(), "alice@example.com", "password1")
	if errors.Is(err, ErrEmailTaken) {
		t.Fatal("Register mapped an unexpected constraint to ErrEmailTaken")
	}
}

func TestRegisterValidatesBeforeDatabase(t *testing.T) {
	db := &fakeDB{query: func(string, ...any) pgx.Row {
		t.Fatal("database must not be reached for invalid input")
		return nil
	}}
	svc := NewService(db)
	if _, err := svc.Register(context.Background(), "not-an-email", "password1"); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("Register = %v, want ErrInvalidEmail", err)
	}
	if _, err := svc.Register(context.Background(), "alice@example.com", "short"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("Register = %v, want ErrInvalidPassword", err)
	}
}

func TestRegisterMissingDatabase(t *testing.T) {
	if _, err := NewService(nil).Register(context.Background(), "alice@example.com", "password1"); err == nil {
		t.Fatal("Register with nil database returned nil error")
	}
}

func TestLoginSuccess(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	id, createdAt := userFixture()
	db := &fakeDB{query: func(sql string, args ...any) pgx.Row {
		if !strings.Contains(sql, "password_hash") {
			t.Fatalf("login lookup SQL missing password_hash: %s", sql)
		}
		return fakeRow{values: []any{id, args[0], "alice", createdAt, string(hash)}}
	}}
	user, err := NewService(db).Login(context.Background(), "Alice@Example.COM", "password1")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if user.Email != "alice@example.com" || user.DisplayName != "alice" || user.ID != id {
		t.Fatalf("logged in user = %#v", user)
	}
}

func TestLoginRejectsUnknownCLIOnlyAndWrongPassword(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	id, createdAt := userFixture()
	tests := []struct {
		name     string
		row      pgx.Row
		password string
	}{
		{"unknown account", noRows(), "password1"},
		{"CLI-only seeded user", fakeRow{values: []any{id, "alice@codeduel.dev", "alice", createdAt, nil}}, "password1"},
		{"wrong password", fakeRow{values: []any{id, "alice@codeduel.dev", "alice", createdAt, string(hash)}}, "wrongpass"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &fakeDB{query: func(string, ...any) pgx.Row { return tt.row }}
			_, err := NewService(db).Login(context.Background(), "alice@codeduel.dev", tt.password)
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("Login = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

func TestLoginCorruptHashIsInternalError(t *testing.T) {
	id, createdAt := userFixture()
	bad := "$2a$10$not-a-valid-hash"
	db := &fakeDB{query: func(string, ...any) pgx.Row {
		return fakeRow{values: []any{id, "alice@example.com", "alice", createdAt, bad}}
	}}
	_, err := NewService(db).Login(context.Background(), "alice@example.com", "password1")
	if errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("Login returned ErrInvalidCredentials for a corrupt stored hash")
	}
	if err == nil {
		t.Fatal("Login accepted a corrupt stored hash")
	}
}

func TestLoginValidatesBeforeDatabase(t *testing.T) {
	db := &fakeDB{query: func(string, ...any) pgx.Row {
		t.Fatal("database must not be reached for invalid input")
		return nil
	}}
	svc := NewService(db)
	if _, err := svc.Login(context.Background(), "not-an-email", "password1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(context.Background(), "alice@example.com", "short"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login = %v, want ErrInvalidCredentials", err)
	}
}

func TestUserByID(t *testing.T) {
	id, createdAt := userFixture()
	db := &fakeDB{query: func(sql string, args ...any) pgx.Row {
		return fakeRow{values: []any{args[0], "alice@example.com", "alice", createdAt}}
	}}
	user, err := NewService(db).UserByID(context.Background(), id)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if user.ID != id || user.Email != "alice@example.com" {
		t.Fatalf("user = %#v", user)
	}

	missing := &fakeDB{query: func(string, ...any) pgx.Row { return noRows() }}
	if _, err := NewService(missing).UserByID(context.Background(), uuid.New()); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("UserByID = %v, want ErrUserNotFound", err)
	}
}

func TestUserJSONExcludesHash(t *testing.T) {
	id, createdAt := userFixture()
	user := User{ID: id, Email: "alice@example.com", DisplayName: "alice", CreatedAt: createdAt}
	encoded, err := json.Marshal(user)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, secret := range []string{"password", "hash", "bcrypt", "password_hash"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("user JSON leaks %q: %s", secret, encoded)
		}
	}
}
