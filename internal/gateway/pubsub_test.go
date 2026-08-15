package gateway

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestUserChannel(t *testing.T) {
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got := userChannel(userID)
	want := "codeduel:user:11111111-1111-1111-1111-111111111111"
	if got != want {
		t.Fatalf("userChannel = %q, want %q", got, want)
	}
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
