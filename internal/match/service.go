package match

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nglong14/CodeDuel/internal/proto"
	"github.com/nglong14/CodeDuel/internal/redisx"
)

const (
	defaultPollInterval  = 250 * time.Millisecond
	defaultRetryInterval = 500 * time.Millisecond
	requeueTimeout       = 2 * time.Second
)

type pairQueue interface {
	PopPair(context.Context) (*redisx.Pair, error)
	Requeue(context.Context, ...redisx.QueueEntry) error
}

type service struct {
	logger        *slog.Logger
	queue         pairQueue
	create        func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error)
	publish       func(context.Context, string, []byte) error
	encode        func(string, any) ([]byte, error)
	pollInterval  time.Duration
	retryInterval time.Duration
	pendingMu     sync.Mutex
	pending       []redisx.QueueEntry
}

func (s *service) run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		delay := s.step(ctx)
		if delay <= 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *service) step(ctx context.Context) time.Duration {
	if !s.restorePending(ctx) {
		return s.retryInterval
	}
	pair, err := s.queue.PopPair(ctx)
	if err != nil {
		s.logger.Error("pop pair failed", "err", err)
		return s.retryInterval
	}
	if pair == nil {
		return s.pollInterval
	}

	players := [2]redisx.QueueMember{pair[0].Member, pair[1].Member}
	created, err := s.create(ctx, players)
	if err != nil {
		var activeErr *ActiveMatchConflictError
		var missingErr *MissingPlayersError
		hasActive := errors.As(err, &activeErr)
		hasMissing := errors.As(err, &missingErr)
		if hasActive || hasMissing {
			if !s.handleIneligiblePlayers(ctx, *pair, activeErr, missingErr) {
				return s.retryInterval
			}
			return 0
		}
		s.logger.Error("create match failed",
			"player_1", players[0].UserID,
			"player_2", players[1].UserID,
			"err", err,
		)
		s.requeueOrRetain(ctx, pair[:]...)
		return s.retryInterval
	}

	payload, err := s.encode(proto.TypeMatchStart, proto.MatchStartData{
		MatchID:   created.MatchID.String(),
		ProblemID: created.ProblemID.String(),
		Deadline:  created.Deadline,
	})
	if err != nil {
		s.logger.Error("encode match start failed", "match_id", created.MatchID, "err", err)
		return s.retryInterval
	}

	publishFailed := false
	for _, player := range players {
		if err := s.publish(ctx, player.Route, payload); err != nil {
			publishFailed = true
			s.logger.Error("publish match start failed",
				"match_id", created.MatchID,
				"user_id", player.UserID,
				"err", err,
			)
		}
	}
	s.logger.Info("match created",
		"match_id", created.MatchID,
		"problem_id", created.ProblemID,
		"player_1", players[0].UserID,
		"player_2", players[1].UserID,
		"deadline", created.Deadline,
	)
	if publishFailed {
		return s.retryInterval
	}
	return 0
}

func (s *service) handleIneligiblePlayers(
	ctx context.Context,
	pair redisx.Pair,
	activeErr *ActiveMatchConflictError,
	missingErr *MissingPlayersError,
) bool {
	eligible := make([]redisx.QueueEntry, 0, len(pair))
	for _, entry := range pair {
		userID := entry.Member.UserID
		if missingErr != nil && missingErr.Has(userID) {
			s.logger.Warn("dropping missing queued player", "user_id", userID)
			continue
		}
		if activeErr != nil {
			if matchID, active := activeErr.MatchFor(userID); active {
				s.logger.Warn("dropping queued player with active match", "user_id", userID, "match_id", matchID)
				s.publishActiveMatchError(ctx, entry.Member.Route, matchID)
				continue
			}
		}
		eligible = append(eligible, entry)
	}

	return s.requeueOrRetain(ctx, eligible...)
}

func (s *service) requeueOrRetain(ctx context.Context, entries ...redisx.QueueEntry) bool {
	if len(entries) == 0 {
		return true
	}
	requeueCtx := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		requeueCtx, cancel = context.WithTimeout(context.Background(), requeueTimeout)
	}
	defer cancel()
	if err := s.queue.Requeue(requeueCtx, entries...); err != nil {
		s.pendingMu.Lock()
		s.pending = append(s.pending, entries...)
		s.pendingMu.Unlock()
		s.logger.Error("requeue players failed", "count", len(entries), "err", err)
		return false
	}
	return true
}

func (s *service) restorePending(ctx context.Context) bool {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if len(s.pending) == 0 {
		return true
	}
	if err := s.queue.Requeue(ctx, s.pending...); err != nil {
		s.logger.Error("restore pending requeue failed", "count", len(s.pending), "err", err)
		return false
	}
	s.pending = nil
	return true
}

func (s *service) publishActiveMatchError(ctx context.Context, route string, matchID uuid.UUID) {
	payload, err := s.encode(proto.TypeError, proto.ErrorData{
		Code:    "already_in_match",
		Message: "player already has an active match",
		MatchID: matchID.String(),
	})
	if err != nil {
		s.logger.Error("encode active match error", "match_id", matchID, "err", err)
		return
	}
	if err := s.publish(ctx, route, payload); err != nil {
		s.logger.Error("publish active match error", "match_id", matchID, "err", err)
	}
}

func newService(
	logger *slog.Logger,
	queue pairQueue,
	create func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error),
	publish func(context.Context, string, []byte) error,
) (*service, error) {
	if logger == nil || queue == nil || create == nil || publish == nil {
		return nil, fmt.Errorf("initialize match service: missing dependency")
	}
	return &service{
		logger:        logger,
		queue:         queue,
		create:        create,
		publish:       publish,
		encode:        proto.Encode,
		pollInterval:  defaultPollInterval,
		retryInterval: defaultRetryInterval,
	}, nil
}
