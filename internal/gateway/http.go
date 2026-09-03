package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/nglong14/CodeDuel/internal/app"
)

const (
	authBodyLimit    = 4 << 10
	readinessTimeout = 2 * time.Second
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type requestError struct {
	status  int
	code    string
	message string
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: apiError{Code: code, Message: message}})
}

func decodeAuthJSON(w http.ResponseWriter, r *http.Request, dst any) *requestError {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &requestError{http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json"}
	}
	r.Body = http.MaxBytesReader(w, r.Body, authBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return &requestError{http.StatusRequestEntityTooLarge, "payload_too_large", "request body is too large"}
		}
		return &requestError{http.StatusBadRequest, "invalid_request", "request body is invalid"}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return &requestError{http.StatusRequestEntityTooLarge, "payload_too_large", "request body is too large"}
		}
		return &requestError{http.StatusBadRequest, "invalid_request", "request body contains trailing data"}
	}
	return nil
}

func handleReadyz(deps *app.Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()
		if deps.Postgres == nil {
			deps.Logger.Warn("readiness check failed", "dependency", "postgres")
			writeAPIError(w, http.StatusServiceUnavailable, "not_ready", "service is not ready")
			return
		}
		if err := deps.Postgres.Ping(ctx); err != nil {
			deps.Logger.Warn("readiness check failed", "dependency", "postgres", "err", err)
			writeAPIError(w, http.StatusServiceUnavailable, "not_ready", "service is not ready")
			return
		}
		if deps.Redis == nil {
			deps.Logger.Warn("readiness check failed", "dependency", "redis")
			writeAPIError(w, http.StatusServiceUnavailable, "not_ready", "service is not ready")
			return
		}
		if err := deps.Redis.Ping(ctx).Err(); err != nil {
			deps.Logger.Warn("readiness check failed", "dependency", "redis", "err", err)
			writeAPIError(w, http.StatusServiceUnavailable, "not_ready", "service is not ready")
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Status string `json:"status"`
		}{Status: "ready"})
	}
}
