package gateway

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nglong14/CodeDuel/internal/app"
	"github.com/nglong14/CodeDuel/internal/redisx"
)

const (
	shutdownTimeout = 5 * time.Second
	presenceTTL     = 75 * time.Second
)

var upgrader = websocket.Upgrader{
	HandshakeTimeout: writeWait,
}

func Run(ctx context.Context, deps *app.Dependencies) error {
	registry := NewRegistry()
	srv := &http.Server{
		Addr:              deps.Config.Gateway.Addr,
		Handler:           newHandler(ctx, deps, registry),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		deps.Logger.Info("ready",
			"addr", srv.Addr,
			"redis_addr", deps.Config.Redis.Addr,
		)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			deps.Logger.Info("http shutdown", "err", err)
		}
		registry.CloseAll()
		registry.Wait()
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return ctx.Err()
		}
		return err
	}
}

func newHandler(ctx context.Context, deps *app.Dependencies, registry *Registry) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /ws", handleWS(ctx, deps, registry))
	return mux
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func handleWS(ctx context.Context, deps *app.Dependencies, registry *Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := Authenticate(r.Context(), r, deps.Config.Gateway.JWTSecret, deps.Postgres)
		if err != nil {
			deps.Logger.Info("unauthorized", "err", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			deps.Logger.Info("upgrade failed", "user_id", userID, "err", err)
			return
		}

		c := newConn(userID, ws, registry)
		c.ctx = ctx
		c.logger = deps.Logger
		queue := redisx.NewQueue(deps.Redis, redisx.DefaultScanLimit)
		c.enqueue = func(callCtx context.Context, member redisx.QueueMember) error {
			_, enqueueErr := queue.Enqueue(callCtx, member)
			return enqueueErr
		}
		c.refreshPresence = func(callCtx context.Context) error {
			refreshed, refreshErr := deps.Redis.Expire(callCtx, c.presenceKey, presenceTTL).Result()
			if refreshErr != nil {
				return refreshErr
			}
			if !refreshed {
				return errors.New("presence lease no longer exists")
			}
			return nil
		}
		if err := deps.Redis.Set(r.Context(), c.presenceKey, "1", presenceTTL).Err(); err != nil {
			deps.Logger.Warn("initial presence failed", "user_id", userID, "err", err)
			c.close()
			return
		}

		unsubscribe, err := subscribeUser(r.Context(), deps.Redis, userID, c)
		if err != nil {
			deps.Logger.Warn("subscribe failed", "user_id", userID, "err", err)
			deletePresence(deps, c.presenceKey)
			c.close()
			return
		}
		c.onClose = func() {
			unsubscribe()
			deletePresence(deps, c.presenceKey)
		}
		if !registry.Add(c) {
			c.cleanup()
			return
		}
		deps.Logger.Info("connected", "user_id", userID, "connection_id", c.connectionID)
		registry.Serve(c)
		deps.Logger.Info("disconnected", "user_id", userID, "connection_id", c.connectionID)
	}
}

func deletePresence(deps *app.Dependencies, key string) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), writeWait)
	defer cancel()
	if err := deps.Redis.Del(cleanupCtx, key).Err(); err != nil {
		deps.Logger.Warn("delete presence failed", "err", err)
	}
}
