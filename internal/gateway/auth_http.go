package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/nglong14/CodeDuel/internal/app"
	accountauth "github.com/nglong14/CodeDuel/internal/auth"
)

type accountService interface {
	Register(context.Context, string, string) (accountauth.User, error)
	Login(context.Context, string, string) (accountauth.User, error)
	UserByID(context.Context, uuid.UUID) (accountauth.User, error)
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionResponse struct {
	Token     string           `json:"token"`
	ExpiresAt time.Time        `json:"expires_at"`
	User      accountauth.User `json:"user"`
}

type meResponse struct {
	User accountauth.User `json:"user"`
}

type authHTTP struct {
	accounts accountService
	secret   string
	tokenTTL time.Duration
	logger   interface {
		Warn(string, ...any)
	}
}

func newAuthHTTP(deps *app.Dependencies) *authHTTP {
	return &authHTTP{
		accounts: accountauth.NewService(deps.Postgres),
		secret:   deps.Config.Gateway.JWTSecret,
		tokenTTL: deps.Config.Auth.TokenTTL,
		logger:   deps.Logger,
	}
}

func (h *authHTTP) register(w http.ResponseWriter, r *http.Request) {
	h.session(w, r, true)
}

func (h *authHTTP) login(w http.ResponseWriter, r *http.Request) {
	h.session(w, r, false)
}

func (h *authHTTP) session(w http.ResponseWriter, r *http.Request, registering bool) {
	w.Header().Set("Cache-Control", "no-store")
	var request credentialsRequest
	if requestErr := decodeAuthJSON(w, r, &request); requestErr != nil {
		writeAPIError(w, requestErr.status, requestErr.code, requestErr.message)
		return
	}
	email, err := accountauth.NormalizeEmail(request.Email)
	if err != nil || accountauth.ValidatePassword(request.Password) != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "email or password is invalid")
		return
	}

	var user accountauth.User
	if registering {
		user, err = h.accounts.Register(r.Context(), email, request.Password)
	} else {
		user, err = h.accounts.Login(r.Context(), email, request.Password)
	}
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	issued, err := MintAccessToken(user.ID, h.secret, h.tokenTTL)
	if err != nil {
		h.logger.Warn("mint auth token failed", "err", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		return
	}
	status := http.StatusOK
	if registering {
		status = http.StatusCreated
	}
	writeJSON(w, status, sessionResponse{Token: issued.Value, ExpiresAt: issued.ExpiresAt, User: user})
}

func (h *authHTTP) me(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	principal, err := AuthenticateREST(r.Context(), r, h.secret, h.accounts)
	if err != nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{User: principal.User})
}

func (h *authHTTP) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, accountauth.ErrEmailTaken):
		writeAPIError(w, http.StatusConflict, "email_taken", "email is already registered")
	case errors.Is(err, accountauth.ErrInvalidEmail), errors.Is(err, accountauth.ErrInvalidPassword):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "email or password is invalid")
	case errors.Is(err, accountauth.ErrInvalidCredentials):
		writeAPIError(w, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
	default:
		h.logger.Warn("auth service failed", "err", fmt.Errorf("service: %w", err))
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}
