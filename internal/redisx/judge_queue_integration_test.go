package redisx

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestJudgeQueueIntegration(t *testing.T) {
	rdb := integrationRedis(t)
	ctx := context.Background()

	t.Run("group sees jobs added before creation and distributes new jobs", func(t *testing.T) {
		flushIntegrationRedis(t, rdb)
		queue := NewJudgeQueue(rdb)
		firstID := uuid.New()
		secondID := uuid.New()
		if _, err := queue.Enqueue(ctx, firstID); err != nil {
			t.Fatalf("enqueue first: %v", err)
		}
		if _, err := queue.Enqueue(ctx, secondID); err != nil {
			t.Fatalf("enqueue second: %v", err)
		}

		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- queue.EnsureGroup(ctx)
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("EnsureGroup: %v", err)
			}
		}

		first, err := queue.Read(ctx, "consumer-one", 1, time.Second)
		if err != nil || len(first) != 1 || first[0].DecodeErr != nil {
			t.Fatalf("first read = %#v, %v", first, err)
		}
		second, err := queue.Read(ctx, "consumer-two", 1, time.Second)
		if err != nil || len(second) != 1 || second[0].DecodeErr != nil {
			t.Fatalf("second read = %#v, %v", second, err)
		}
		if first[0].SubmissionID == second[0].SubmissionID {
			t.Fatalf("both consumers received submission %s", first[0].SubmissionID)
		}

		for _, job := range append(first, second...) {
			if err := queue.Ack(ctx, job.EntryID); err != nil {
				t.Fatalf("Ack: %v", err)
			}
			if err := queue.Delete(ctx, job.EntryID); err != nil {
				t.Fatalf("Delete: %v", err)
			}
		}
		pending, err := rdb.XPending(ctx, JudgeJobsKey, JudgeConsumerGroup).Result()
		if err != nil {
			t.Fatalf("XPENDING: %v", err)
		}
		if pending.Count != 0 || rdb.XLen(ctx, JudgeJobsKey).Val() != 0 {
			t.Fatalf("pending = %d, stream length = %d", pending.Count, rdb.XLen(ctx, JudgeJobsKey).Val())
		}
	})

	t.Run("malformed entries retain acknowledgment decision", func(t *testing.T) {
		flushIntegrationRedis(t, rdb)
		queue := NewJudgeQueue(rdb)
		if err := queue.EnsureGroup(ctx); err != nil {
			t.Fatalf("EnsureGroup: %v", err)
		}
		if _, err := rdb.XAdd(ctx, &redis.XAddArgs{Stream: JudgeJobsKey, Values: map[string]any{"schema_version": "1"}}).Result(); err != nil {
			t.Fatalf("XADD malformed job: %v", err)
		}
		jobs, err := queue.Read(ctx, "consumer", 1, time.Second)
		if err != nil || len(jobs) != 1 || jobs[0].DecodeErr == nil || jobs[0].EntryID == "" {
			t.Fatalf("Read malformed = %#v, %v", jobs, err)
		}
		if err := queue.Ack(ctx, jobs[0].EntryID); err != nil {
			t.Fatalf("Ack malformed: %v", err)
		}
	})

	t.Run("cancellation interrupts blocking read", func(t *testing.T) {
		flushIntegrationRedis(t, rdb)
		queue := NewJudgeQueue(rdb)
		if err := queue.EnsureGroup(ctx); err != nil {
			t.Fatalf("EnsureGroup: %v", err)
		}
		readCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			_, err := queue.Read(readCtx, "consumer", 1, 10*time.Second)
			done <- err
		}()
		time.Sleep(20 * time.Millisecond)
		cancel()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("Read returned nil error after cancellation")
			}
		case <-time.After(time.Second):
			t.Fatal("Read did not stop after cancellation")
		}
	})
}
