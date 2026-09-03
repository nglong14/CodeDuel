package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

const (
	minPasswordBytes = 8
	maxPasswordBytes = 72
	maxEmailBytes    = 254
	maxDisplayRunes  = 64
	// Generated at bcrypt.DefaultCost and used to equalize missing-account login work.
	dummyPasswordHash = "$2a$10$7EqJtq98hPqEX7fNZaFWoO5tO6M3jI1Q11khFhRvpZB2M7fS5QwNm"
)

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrInvalidPassword    = errors.New("invalid password")
	ErrEmailTaken         = errors.New("email taken")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserNotFound       = errors.New("user not found")
)

type User struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type DB interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Service struct {
	db DB
}

func NewService(db DB) *Service {
	if db != nil {
		value := reflect.ValueOf(db)
		if value.Kind() == reflect.Pointer && value.IsNil() {
			db = nil
		}
	}
	return &Service{db: db}
}

func NormalizeEmail(raw string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if !utf8.ValidString(normalized) || len(normalized) < 3 || len(normalized) > maxEmailBytes {
		return "", ErrInvalidEmail
	}
	address, err := mail.ParseAddress(normalized)
	if err != nil || address.Address != normalized || strings.Count(normalized, "@") != 1 {
		return "", ErrInvalidEmail
	}
	local, domain, ok := strings.Cut(normalized, "@")
	if !ok || local == "" || domain == "" || strings.TrimSpace(local) != local || utf8.RuneCountInString(local) > maxDisplayRunes {
		return "", ErrInvalidEmail
	}
	return normalized, nil
}

func ValidatePassword(password string) error {
	if !utf8.ValidString(password) || len(password) < minPasswordBytes || len(password) > maxPasswordBytes {
		return ErrInvalidPassword
	}
	return nil
}

func (s *Service) Register(ctx context.Context, rawEmail, password string) (User, error) {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return User{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}
	if s == nil || s.db == nil {
		return User{}, errors.New("register user: missing database")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("register user: hash password: %w", err)
	}
	displayName, _, _ := strings.Cut(email, "@")
	var user User
	err = s.db.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, display_name)
		VALUES ($1, $2, $3)
		RETURNING id, email, display_name, created_at
	`, email, string(hash), displayName).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
			return User{}, ErrEmailTaken
		}
		return User{}, fmt.Errorf("register user: insert: %w", err)
	}
	return user, nil
}

func (s *Service) Login(ctx context.Context, rawEmail, password string) (User, error) {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return User{}, ErrInvalidCredentials
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, ErrInvalidCredentials
	}
	if s == nil || s.db == nil {
		return User{}, errors.New("login user: missing database")
	}

	var user User
	var passwordHash *string
	err = s.db.QueryRow(ctx, `
		SELECT id, email, display_name, created_at, password_hash
		FROM users
		WHERE email = $1
	`, email).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		if compareErr := bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(password)); compareErr != nil && !errors.Is(compareErr, bcrypt.ErrMismatchedHashAndPassword) {
			return User{}, fmt.Errorf("login user: dummy password comparison: %w", compareErr)
		}
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("login user: lookup: %w", err)
	}
	if passwordHash == nil {
		if compareErr := bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(password)); compareErr != nil && !errors.Is(compareErr, bcrypt.ErrMismatchedHashAndPassword) {
			return User{}, fmt.Errorf("login user: dummy password comparison: %w", compareErr)
		}
		return User{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*passwordHash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return User{}, ErrInvalidCredentials
		}
		return User{}, fmt.Errorf("login user: compare password: %w", err)
	}
	return user, nil
}

func (s *Service) UserByID(ctx context.Context, id uuid.UUID) (User, error) {
	if s == nil || s.db == nil {
		return User{}, errors.New("get user: missing database")
	}
	var user User
	err := s.db.QueryRow(ctx, `
		SELECT id, email, display_name, created_at
		FROM users
		WHERE id = $1
	`, id).Scan(&user.ID, &user.Email, &user.DisplayName, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: lookup: %w", err)
	}
	return user, nil
}
