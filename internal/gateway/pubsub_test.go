package gateway

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/nglong14/CodeDuel/internal/proto"
	"github.com/nglong14/CodeDuel/internal/redisx"
)

func TestUserChannel(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got := userChannel(userID)
	want := "codeduel:user:11111111-1111-1111-1111-111111111111"
	if got != want {
		t.Fatalf("userChannel = %q, want %q", got, want)
	}
}

func TestSubscribeUserFailureDoesNotReturnSubscription(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr: "unused",
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("subscription unavailable")
		},
	})
	t.Cleanup(func() { _ = rdb.Close() })

	sub, err := subscribeUser(context.Background(), rdb, uuid.New())
	if err == nil {
		t.Fatal("subscribeUser returned nil error")
	}
	if sub != nil {
		t.Fatal("subscribeUser returned a subscription after confirmation failure")
	}
}

func TestFanoutCannotOvertakeInboundResponse(t *testing.T) {
	c := newConn(uuid.New(), nil, NewRegistry())
	enqueueStarted := make(chan struct{})
	releaseEnqueue := make(chan struct{})
	c.enqueue = func(context.Context, redisx.QueueMember) error {
		close(enqueueStarted)
		<-releaseEnqueue
		return nil
	}
	join, err := proto.Encode(proto.TypeJoinQueue, nil)
	if err != nil {
		t.Fatalf("Encode join: %v", err)
	}

	handled := make(chan error, 1)
	go func() { handled <- c.handleAndSend(join) }()
	<-enqueueStarted

	ch := make(chan *redis.Message, 1)
	fanoutDone := make(chan struct{})
	go func() {
		fanout(ch, c)
		close(fanoutDone)
	}()
	matchStart, err := proto.Encode(proto.TypeMatchStart, proto.MatchStartData{MatchID: uuid.NewString()})
	if err != nil {
		t.Fatalf("Encode match_start: %v", err)
	}
	ch <- &redis.Message{Payload: string(matchStart)}

	select {
	case raw := <-c.send:
		t.Fatalf("delivery completed before enqueue returned: %s", raw)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseEnqueue)
	if err := <-handled; err != nil {
		t.Fatalf("handleAndSend: %v", err)
	}
	assertEnvelopeType(t, <-c.send, proto.TypeQueued)
	assertEnvelopeType(t, <-c.send, proto.TypeMatchStart)

	close(ch)
	select {
	case <-fanoutDone:
	case <-time.After(time.Second):
		t.Fatal("fanout did not exit")
	}
}

func TestReadyQueuedBeforeFanoutStarts(t *testing.T) {
	c := newConn(uuid.New(), nil, NewRegistry())
	ready, err := proto.Encode(proto.TypeReady, proto.ReadyData{UserID: c.userID.String()})
	if err != nil {
		t.Fatalf("Encode ready: %v", err)
	}
	c.Send(ready)

	ch := make(chan *redis.Message, 1)
	done := make(chan struct{})
	go func() {
		fanout(ch, c)
		close(done)
	}()
	ch <- &redis.Message{Payload: `{"type":"match_start"}`}
	close(ch)

	assertEnvelopeType(t, <-c.send, proto.TypeReady)
	assertEnvelopeType(t, <-c.send, proto.TypeMatchStart)
	<-done
}

func TestFanoutPushesPayload(t *testing.T) {
	c := newConn(uuid.MustParse("11111111-1111-1111-1111-111111111111"), nil, NewRegistry())
	ch := make(chan *redis.Message, 1)
	done := make(chan struct{})
	go func() {
		fanout(ch, c)
		close(done)
	}()

	payload := `{"type":"match_start","data":{"match_id":"m1"}}`
	ch <- &redis.Message{Payload: payload}

	select {
	case got := <-c.send:
		if string(got) != payload {
			t.Fatalf("payload = %q, want %q", got, payload)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fanout")
	}

	close(ch)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fanout did not exit after channel close")
	}
}

func TestFanoutStopsWhenChannelCloses(t *testing.T) {
	c := newConn(uuid.MustParse("11111111-1111-1111-1111-111111111111"), nil, NewRegistry())
	c.close()
	ch := make(chan *redis.Message, 1)
	done := make(chan struct{})
	go func() {
		fanout(ch, c)
		close(done)
	}()

	ch <- &redis.Message{Payload: `{"type":"judging"}`}
	close(ch)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fanout did not exit")
	}
}
