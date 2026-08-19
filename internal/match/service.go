package match

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
	Requeue(context.Context, redisx.Pair) error
}

type service struct {
	logger        *slog.Logger
	queue         pairQueue
	create        func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error)
	publish       func(context.Context, string, []byte) error
	encode        func(string, any) ([]byte, error)
	pollInterval  time.Duration
	retryInterval time.Duration
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
		s.logger.Error("create match failed",
			"player_1", players[0].UserID,
			"player_2", players[1].UserID,
			"err", err,
		)
		requeueCtx := ctx
		cancel := func() {}
		if ctx.Err() != nil {
			requeueCtx, cancel = context.WithTimeout(context.Background(), requeueTimeout)
		}
		defer cancel()
		if requeueErr := s.queue.Requeue(requeueCtx, *pair); requeueErr != nil {
			s.logger.Error("requeue pair failed", "err", requeueErr)
		}
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
