package match

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nglong14/CodeDuel/internal/proto"
	"github.com/nglong14/CodeDuel/internal/redisx"
)

type fakeQueue struct {
	pair         *redisx.Pair
	popErr       error
	requeueErr   error
	requeueCalls int
	requeued     []redisx.QueueEntry
}

func (q *fakeQueue) PopPair(context.Context) (*redisx.Pair, error) {
	pair := q.pair
	q.pair = nil
	return pair, q.popErr
}

func (q *fakeQueue) Requeue(_ context.Context, entries ...redisx.QueueEntry) error {
	q.requeueCalls++
	q.requeued = append(q.requeued, entries...)
	return q.requeueErr
}

func TestServicePublishesIdenticalMatchStart(t *testing.T) {
	pair := testPair()
	queue := &fakeQueue{pair: &pair}
	created := CreatedMatch{
		MatchID:   uuid.New(),
		ProblemID: uuid.New(),
		Deadline:  time.Now().UTC().Add(time.Minute),
		Players:   testPlayers(),
	}
	var routes []string
	var payloads [][]byte
	service := mustService(t, queue,
		func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error) { return created, nil },
		func(_ context.Context, route string, payload []byte) error {
			routes = append(routes, route)
			payloads = append(payloads, append([]byte(nil), payload...))
			return nil
		},
	)

	if delay := service.step(context.Background()); delay != 0 {
		t.Fatalf("step delay = %v, want 0", delay)
	}
	if len(payloads) != 2 || string(payloads[0]) != string(payloads[1]) {
		t.Fatalf("payloads = %q", payloads)
	}
	if routes[0] != pair[0].Member.Route || routes[1] != pair[1].Member.Route {
		t.Fatalf("routes = %v", routes)
	}
	env, err := proto.Decode(payloads[0])
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if env.Type != proto.TypeMatchStart {
		t.Fatalf("type = %q, want %q", env.Type, proto.TypeMatchStart)
	}
	var data proto.MatchStartData
	if err := env.DecodeData(&data); err != nil {
		t.Fatalf("DecodeData: %v", err)
	}
	if data.MatchID != created.MatchID.String() || data.ProblemID != created.ProblemID.String() || !data.Deadline.Equal(created.Deadline) {
		t.Fatalf("match_start data = %#v", data)
	}
}

func TestServiceRequeuesOnlyCreationFailures(t *testing.T) {
	t.Run("creation failure", func(t *testing.T) {
		pair := testPair()
		queue := &fakeQueue{pair: &pair}
		publishCalls := 0
		service := mustService(t, queue,
			func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error) {
				return CreatedMatch{}, errors.New("database unavailable")
			},
			func(context.Context, string, []byte) error { publishCalls++; return nil },
		)
		if delay := service.step(context.Background()); delay != service.retryInterval {
			t.Fatalf("step delay = %v, want %v", delay, service.retryInterval)
		}
		if queue.requeueCalls != 1 || len(queue.requeued) != len(pair) || publishCalls != 0 {
			t.Fatalf("requeue calls = %d, entries = %d, publish calls = %d", queue.requeueCalls, len(queue.requeued), publishCalls)
		}
	})

	t.Run("publish failure", func(t *testing.T) {
		pair := testPair()
		queue := &fakeQueue{pair: &pair}
		publishCalls := 0
		service := mustService(t, queue,
			func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error) {
				return CreatedMatch{MatchID: uuid.New(), ProblemID: uuid.New(), Deadline: time.Now()}, nil
			},
			func(context.Context, string, []byte) error { publishCalls++; return errors.New("redis unavailable") },
		)
		service.step(context.Background())
		if queue.requeueCalls != 0 || publishCalls != 2 {
			t.Fatalf("requeue calls = %d, publish calls = %d", queue.requeueCalls, publishCalls)
		}
	})

	t.Run("encoding failure", func(t *testing.T) {
		pair := testPair()
		queue := &fakeQueue{pair: &pair}
		service := mustService(t, queue,
			func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error) {
				return CreatedMatch{MatchID: uuid.New(), ProblemID: uuid.New(), Deadline: time.Now()}, nil
			},
			func(context.Context, string, []byte) error { t.Fatal("publish called"); return nil },
		)
		service.encode = func(string, any) ([]byte, error) { return nil, errors.New("encode") }
		service.step(context.Background())
		if queue.requeueCalls != 0 {
			t.Fatalf("requeue calls = %d, want 0", queue.requeueCalls)
		}
	})
}

func TestServiceDropsIneligiblePlayersAndRequeuesOpponent(t *testing.T) {
	t.Run("active player", func(t *testing.T) {
		pair := testPair()
		queue := &fakeQueue{pair: &pair}
		activeMatchID := uuid.New()
		var publishedRoute string
		var publishedPayload []byte
		service := mustService(t, queue,
			func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error) {
				return CreatedMatch{}, &ActiveMatchConflictError{Claims: []ActiveMatchClaim{{
					UserID: pair[0].Member.UserID, MatchID: activeMatchID,
				}}}
			},
			func(_ context.Context, route string, payload []byte) error {
				publishedRoute = route
				publishedPayload = append([]byte(nil), payload...)
				return nil
			},
		)

		if delay := service.step(context.Background()); delay != 0 {
			t.Fatalf("step delay = %v, want 0", delay)
		}
		if queue.requeueCalls != 1 || len(queue.requeued) != 1 || queue.requeued[0].Member.UserID != pair[1].Member.UserID {
			t.Fatalf("requeued = %#v, calls = %d", queue.requeued, queue.requeueCalls)
		}
		if publishedRoute != pair[0].Member.Route {
			t.Fatalf("published route = %q, want %q", publishedRoute, pair[0].Member.Route)
		}
		env, err := proto.Decode(publishedPayload)
		if err != nil {
			t.Fatalf("Decode active error: %v", err)
		}
		var data proto.ErrorData
		if env.Type != proto.TypeError || env.DecodeData(&data) != nil || data.Code != "already_in_match" || data.MatchID != activeMatchID.String() {
			t.Fatalf("active error = type %q data %#v", env.Type, data)
		}
	})

	t.Run("missing player", func(t *testing.T) {
		pair := testPair()
		queue := &fakeQueue{pair: &pair}
		service := mustService(t, queue,
			func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error) {
				return CreatedMatch{}, &MissingPlayersError{PlayerIDs: []uuid.UUID{pair[0].Member.UserID}}
			},
			func(context.Context, string, []byte) error {
				t.Fatal("publish called for missing player")
				return nil
			},
		)

		if delay := service.step(context.Background()); delay != 0 {
			t.Fatalf("step delay = %v, want 0", delay)
		}
		if queue.requeueCalls != 1 || len(queue.requeued) != 1 || queue.requeued[0].Member.UserID != pair[1].Member.UserID {
			t.Fatalf("requeued = %#v, calls = %d", queue.requeued, queue.requeueCalls)
		}
	})

	t.Run("combined active and missing", func(t *testing.T) {
		pair := testPair()
		queue := &fakeQueue{pair: &pair}
		service := mustService(t, queue,
			func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error) {
				return CreatedMatch{}, errors.Join(
					&ActiveMatchConflictError{Claims: []ActiveMatchClaim{{UserID: pair[0].Member.UserID, MatchID: uuid.New()}}},
					&MissingPlayersError{PlayerIDs: []uuid.UUID{pair[1].Member.UserID}},
				)
			},
			func(context.Context, string, []byte) error { return nil },
		)

		if delay := service.step(context.Background()); delay != 0 {
			t.Fatalf("step delay = %v, want 0", delay)
		}
		if queue.requeueCalls != 0 || len(queue.requeued) != 0 {
			t.Fatalf("requeued = %#v, calls = %d", queue.requeued, queue.requeueCalls)
		}
	})

	t.Run("failed opponent requeue is retried before another pop", func(t *testing.T) {
		pair := testPair()
		queue := &fakeQueue{pair: &pair, requeueErr: errors.New("redis unavailable")}
		activeMatchID := uuid.New()
		createCalls := 0
		service := mustService(t, queue,
			func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error) {
				createCalls++
				return CreatedMatch{}, &ActiveMatchConflictError{Claims: []ActiveMatchClaim{{
					UserID: pair[0].Member.UserID, MatchID: activeMatchID,
				}}}
			},
			func(context.Context, string, []byte) error { return nil },
		)

		if delay := service.step(context.Background()); delay != service.retryInterval {
			t.Fatalf("first step delay = %v, want %v", delay, service.retryInterval)
		}
		if len(service.pending) != 1 || service.pending[0].Member.UserID != pair[1].Member.UserID {
			t.Fatalf("pending requeue = %#v", service.pending)
		}
		queue.requeueErr = nil
		if delay := service.step(context.Background()); delay != service.pollInterval {
			t.Fatalf("retry step delay = %v, want %v", delay, service.pollInterval)
		}
		if len(service.pending) != 0 || queue.requeueCalls != 2 || createCalls != 1 {
			t.Fatalf("pending = %#v, requeue calls = %d, create calls = %d", service.pending, queue.requeueCalls, createCalls)
		}
	})
}

func TestServiceRunStopsDuringPollWait(t *testing.T) {
	queue := &fakeQueue{}
	service := mustService(t, queue,
		func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error) {
			t.Fatal("create called")
			return CreatedMatch{}, nil
		},
		func(context.Context, string, []byte) error { t.Fatal("publish called"); return nil },
	)
	service.pollInterval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.run(ctx) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("run error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("service did not stop")
	}
}

func mustService(
	t *testing.T,
	queue pairQueue,
	create func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error),
	publish func(context.Context, string, []byte) error,
) *service {
	t.Helper()
	service, err := newService(slog.New(slog.DiscardHandler), queue, create, publish)
	if err != nil {
		t.Fatalf("newService: %v", err)
	}
	return service
}

func testPair() redisx.Pair {
	players := testPlayers()
	return redisx.Pair{
		{Member: players[0], Score: 1},
		{Member: players[1], Score: 2},
	}
}
