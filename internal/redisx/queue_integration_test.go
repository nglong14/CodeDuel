package redisx

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestQueueIntegration(t *testing.T) {
	rdb := integrationRedis(t)
	ctx := context.Background()

	t.Run("empty and single live member", func(t *testing.T) {
		flushIntegrationRedis(t, rdb)
		queue := NewQueue(rdb, DefaultScanLimit)
		if pair, err := queue.PopPair(ctx); err != nil || pair != nil {
			t.Fatalf("empty PopPair = %#v, %v", pair, err)
		}
		member := liveMember(t, rdb)
		if _, err := queue.Enqueue(ctx, member); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
		if pair, err := queue.PopPair(ctx); err != nil || pair != nil {
			t.Fatalf("single PopPair = %#v, %v", pair, err)
		}
		if got := rdb.ZCard(ctx, QueueKey).Val(); got != 1 {
			t.Fatalf("queue size = %d, want 1", got)
		}
	})

	t.Run("FIFO duplicate and reconnect", func(t *testing.T) {
		flushIntegrationRedis(t, rdb)
		queue := NewQueue(rdb, DefaultScanLimit)
		first := liveMember(t, rdb)
		initial, err := queue.Enqueue(ctx, first)
		if err != nil {
			t.Fatalf("Enqueue first: %v", err)
		}
		duplicate, err := queue.Enqueue(ctx, first)
		if err != nil {
			t.Fatalf("Enqueue duplicate: %v", err)
		}
		if duplicate.Added || duplicate.Score != initial.Score {
			t.Fatalf("duplicate = %#v, initial = %#v", duplicate, initial)
		}

		first.PresenceKey = PresenceKey(first.UserID, uuid.New())
		if err := rdb.Set(ctx, first.PresenceKey, "1", time.Minute).Err(); err != nil {
			t.Fatalf("set reconnect presence: %v", err)
		}
		reconnected, err := queue.Enqueue(ctx, first)
		if err != nil {
			t.Fatalf("Enqueue reconnect: %v", err)
		}
		if reconnected.Added || reconnected.Score != initial.Score {
			t.Fatalf("reconnected = %#v, initial = %#v", reconnected, initial)
		}

		second := liveMember(t, rdb)
		if _, err := queue.Enqueue(ctx, second); err != nil {
			t.Fatalf("Enqueue second: %v", err)
		}
		pair, err := queue.PopPair(ctx)
		if err != nil {
			t.Fatalf("PopPair: %v", err)
		}
		if pair == nil || pair[0].Member.UserID != first.UserID || pair[1].Member.UserID != second.UserID {
			t.Fatalf("pair = %#v", pair)
		}
		if got := rdb.ZCard(ctx, QueueKey).Val(); got != 0 {
			t.Fatalf("queue size = %d, want 0", got)
		}
	})

	t.Run("malformed and expired members do not block", func(t *testing.T) {
		flushIntegrationRedis(t, rdb)
		queue := NewQueue(rdb, DefaultScanLimit)
		if err := rdb.ZAdd(ctx, QueueKey, redis.Z{Score: 0, Member: "not-json"}).Err(); err != nil {
			t.Fatalf("ZAdd malformed: %v", err)
		}
		stale := liveMember(t, rdb)
		staleRaw, err := encodeMember(stale)
		if err != nil {
			t.Fatalf("encode stale: %v", err)
		}
		if err := rdb.Del(ctx, stale.PresenceKey).Err(); err != nil {
			t.Fatalf("delete stale presence: %v", err)
		}
		if err := rdb.ZAdd(ctx, QueueKey, redis.Z{Score: 1, Member: staleRaw}).Err(); err != nil {
			t.Fatalf("ZAdd stale: %v", err)
		}
		if err := rdb.HSet(ctx, MembersKey, stale.UserID.String(), staleRaw).Err(); err != nil {
			t.Fatalf("HSet stale: %v", err)
		}
		first := liveMember(t, rdb)
		second := liveMember(t, rdb)
		if _, err := queue.Enqueue(ctx, first); err != nil {
			t.Fatalf("Enqueue first: %v", err)
		}
		if _, err := queue.Enqueue(ctx, second); err != nil {
			t.Fatalf("Enqueue second: %v", err)
		}
		pair, err := queue.PopPair(ctx)
		if err != nil {
			t.Fatalf("PopPair: %v", err)
		}
		if pair == nil || pair[0].Member.UserID != first.UserID || pair[1].Member.UserID != second.UserID {
			t.Fatalf("pair = %#v", pair)
		}
		if got := rdb.ZCard(ctx, QueueKey).Val(); got != 0 {
			t.Fatalf("queue size = %d, want 0", got)
		}
	})

	t.Run("requeue restores scores without replacing reconnect", func(t *testing.T) {
		flushIntegrationRedis(t, rdb)
		queue := NewQueue(rdb, DefaultScanLimit)
		first := liveMember(t, rdb)
		second := liveMember(t, rdb)
		firstResult, _ := queue.Enqueue(ctx, first)
		secondResult, _ := queue.Enqueue(ctx, second)
		pair, err := queue.PopPair(ctx)
		if err != nil || pair == nil {
			t.Fatalf("PopPair = %#v, %v", pair, err)
		}
		if err := queue.Requeue(ctx, pair[:]...); err != nil {
			t.Fatalf("Requeue: %v", err)
		}
		if got := int64(rdb.ZScore(ctx, QueueKey, pair[0].encoded).Val()); got != firstResult.Score {
			t.Fatalf("first score = %d, want %d", got, firstResult.Score)
		}
		if got := int64(rdb.ZScore(ctx, QueueKey, pair[1].encoded).Val()); got != secondResult.Score {
			t.Fatalf("second score = %d, want %d", got, secondResult.Score)
		}

		poppedAgain, err := queue.PopPair(ctx)
		if err != nil || poppedAgain == nil {
			t.Fatalf("second PopPair = %#v, %v", poppedAgain, err)
		}
		first.PresenceKey = PresenceKey(first.UserID, uuid.New())
		if err := rdb.Set(ctx, first.PresenceKey, "1", time.Minute).Err(); err != nil {
			t.Fatalf("set reconnect presence: %v", err)
		}
		if _, err := queue.Enqueue(ctx, first); err != nil {
			t.Fatalf("Enqueue reconnect: %v", err)
		}
		if err := rdb.Del(ctx, second.PresenceKey).Err(); err != nil {
			t.Fatalf("delete second presence: %v", err)
		}
		if err := queue.Requeue(ctx, poppedAgain[:]...); err != nil {
			t.Fatalf("Requeue replaced pair: %v", err)
		}
		if got := rdb.ZCard(ctx, QueueKey).Val(); got != 1 {
			t.Fatalf("queue size = %d, want only reconnect", got)
		}
	})

	t.Run("partial requeue restores one entry with original score", func(t *testing.T) {
		flushIntegrationRedis(t, rdb)
		queue := NewQueue(rdb, DefaultScanLimit)
		first := liveMember(t, rdb)
		second := liveMember(t, rdb)
		firstResult, err := queue.Enqueue(ctx, first)
		if err != nil {
			t.Fatalf("Enqueue first: %v", err)
		}
		if _, err := queue.Enqueue(ctx, second); err != nil {
			t.Fatalf("Enqueue second: %v", err)
		}
		pair, err := queue.PopPair(ctx)
		if err != nil || pair == nil {
			t.Fatalf("PopPair = %#v, %v", pair, err)
		}

		if err := queue.Requeue(ctx, pair[0]); err != nil {
			t.Fatalf("Requeue one entry: %v", err)
		}
		if got := rdb.ZCard(ctx, QueueKey).Val(); got != 1 {
			t.Fatalf("queue size = %d, want 1", got)
		}
		if got := rdb.HLen(ctx, MembersKey).Val(); got != 1 {
			t.Fatalf("member mappings = %d, want 1", got)
		}
		if got := int64(rdb.ZScore(ctx, QueueKey, pair[0].encoded).Val()); got != firstResult.Score {
			t.Fatalf("restored score = %d, want %d", got, firstResult.Score)
		}
		if got := rdb.HGet(ctx, MembersKey, second.UserID.String()).Val(); got != "" {
			t.Fatalf("second member mapping = %q, want empty", got)
		}
	})

	t.Run("partial requeue does not replace reconnect", func(t *testing.T) {
		flushIntegrationRedis(t, rdb)
		queue := NewQueue(rdb, DefaultScanLimit)
		first := liveMember(t, rdb)
		second := liveMember(t, rdb)
		if _, err := queue.Enqueue(ctx, first); err != nil {
			t.Fatalf("Enqueue first: %v", err)
		}
		if _, err := queue.Enqueue(ctx, second); err != nil {
			t.Fatalf("Enqueue second: %v", err)
		}
		pair, err := queue.PopPair(ctx)
		if err != nil || pair == nil {
			t.Fatalf("PopPair = %#v, %v", pair, err)
		}

		first.PresenceKey = PresenceKey(first.UserID, uuid.New())
		if err := rdb.Set(ctx, first.PresenceKey, "1", time.Minute).Err(); err != nil {
			t.Fatalf("set reconnect presence: %v", err)
		}
		reconnected, err := queue.Enqueue(ctx, first)
		if err != nil {
			t.Fatalf("Enqueue reconnect: %v", err)
		}
		if err := queue.Requeue(ctx, pair[0]); err != nil {
			t.Fatalf("Requeue old entry: %v", err)
		}

		reconnectedRaw, err := encodeMember(first)
		if err != nil {
			t.Fatalf("encode reconnect: %v", err)
		}
		if got := rdb.HGet(ctx, MembersKey, first.UserID.String()).Val(); got != reconnectedRaw {
			t.Fatalf("member mapping = %q, want reconnect %q", got, reconnectedRaw)
		}
		if got := rdb.ZCard(ctx, QueueKey).Val(); got != 1 {
			t.Fatalf("queue size = %d, want 1", got)
		}
		if got := int64(rdb.ZScore(ctx, QueueKey, reconnectedRaw).Val()); got != reconnected.Score {
			t.Fatalf("reconnect score = %d, want %d", got, reconnected.Score)
		}
	})

	t.Run("script cache flush recovers", func(t *testing.T) {
		flushIntegrationRedis(t, rdb)
		queue := NewQueue(rdb, DefaultScanLimit)
		if _, err := queue.Enqueue(ctx, liveMember(t, rdb)); err != nil {
			t.Fatalf("initial Enqueue: %v", err)
		}
		if err := rdb.ScriptFlush(ctx).Err(); err != nil {
			t.Fatalf("ScriptFlush: %v", err)
		}
		if _, err := queue.Enqueue(ctx, liveMember(t, rdb)); err != nil {
			t.Fatalf("Enqueue after ScriptFlush: %v", err)
		}
		if pair, err := queue.PopPair(ctx); err != nil || pair == nil {
			t.Fatalf("PopPair after ScriptFlush = %#v, %v", pair, err)
		}
	})
}

func integrationRedis(t *testing.T) *redis.Client {
	t.Helper()
	if os.Getenv("CODEDUEL_INTEGRATION") != "1" {
		t.Skip("set CODEDUEL_INTEGRATION=1 to run integration tests")
	}
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		_ = rdb.Close()
		t.Fatalf("connect to integration Redis: %v", err)
	}
	t.Cleanup(func() {
		_ = rdb.FlushDB(context.Background()).Err()
		_ = rdb.Close()
	})
	return rdb
}

func flushIntegrationRedis(t *testing.T, rdb *redis.Client) {
	t.Helper()
	if err := rdb.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("FlushDB integration database: %v", err)
	}
}

func liveMember(t *testing.T, rdb *redis.Client) QueueMember {
	t.Helper()
	userID := uuid.New()
	member := QueueMember{
		UserID:      userID,
		PresenceKey: PresenceKey(userID, uuid.New()),
		Route:       UserChannel(userID),
	}
	if err := rdb.Set(context.Background(), member.PresenceKey, "1", time.Minute).Err(); err != nil {
		t.Fatalf("set presence: %v", err)
	}
	return member
}
