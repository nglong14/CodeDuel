package redisx

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestQueueConcurrentPopIntegration(t *testing.T) {
	rdb := integrationRedis(t)
	flushIntegrationRedis(t, rdb)
	queue := NewQueue(rdb, DefaultScanLimit)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const users = 1000
	expected := make(map[uuid.UUID]struct{}, users)
	for range users {
		member := liveMember(t, rdb)
		expected[member.UserID] = struct{}{}
		if _, err := queue.Enqueue(ctx, member); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	seen := make(map[uuid.UUID]struct{}, users)
	var mu sync.Mutex
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				done := len(seen) == users
				mu.Unlock()
				if done {
					return
				}
				pair, err := queue.PopPair(ctx)
				if err != nil {
					if ctx.Err() == nil {
						errCh <- err
					}
					return
				}
				if pair == nil {
					runtime.Gosched()
					continue
				}
				mu.Lock()
				for _, entry := range pair {
					if _, duplicate := seen[entry.Member.UserID]; duplicate {
						mu.Unlock()
						errCh <- fmt.Errorf("user %s popped twice", entry.Member.UserID)
						return
					}
					seen[entry.Member.UserID] = struct{}{}
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if len(seen) != len(expected) {
		t.Fatalf("popped users = %d, want %d", len(seen), len(expected))
	}
	if got := rdb.ZCard(context.Background(), QueueKey).Val(); got != 0 {
		t.Fatalf("queue size = %d, want 0", got)
	}
	if got := rdb.HLen(context.Background(), MembersKey).Val(); got != 0 {
		t.Fatalf("member mappings = %d, want 0", got)
	}
}
