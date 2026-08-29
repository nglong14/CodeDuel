package judge

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nglong14/CodeDuel/internal/redisx"
)

func TestRunJudgeWorkersBoundsConcurrency(t *testing.T) {
	jobs := make([]redisx.JudgeJob, 4)
	for index := range jobs {
		jobs[index] = redisx.JudgeJob{EntryID: uuid.NewString(), SubmissionID: uuid.New()}
	}
	queue := newMultiJobQueue(jobs)
	store := concurrentTestStore{}
	var active, maximum, executions atomic.Int32
	started := make(chan struct{}, len(jobs))
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorkers := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseWorkers()
	executor := executorFunc(func(context.Context, ExecutionRequest) (ExecutionOutcome, error) {
		current := active.Add(1)
		defer active.Add(-1)
		executions.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		return ExecutionOutcome{Kind: OutcomeWrongAnswer, TestsPassed: 2}, nil
	})
	workers := make([]*judgeService, 2)
	for index := range workers {
		workers[index] = newTestJudgeService(queue, store, executor, func(context.Context, string, []byte) error { return nil })
		workers[index].consumer = "bounded-consumer-" + uuid.NewString()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runJudgeWorkers(ctx, workers, time.Second)
	}()
	receiveJudgeTest(t, started, "first worker start")
	receiveJudgeTest(t, started, "second worker start")
	releaseWorkers()
	receiveJudgeTest(t, queue.allDeleted, "all jobs finalized")
	cancel()
	if err := receiveJudgeTest(t, done, "bounded workers stop"); !errors.Is(err, context.Canceled) {
		t.Fatalf("runJudgeWorkers error = %v, want context cancellation", err)
	}
	if executions.Load() != int32(len(jobs)) || maximum.Load() != int32(len(workers)) {
		t.Fatalf("executions/max concurrency = %d/%d, want %d/%d",
			executions.Load(), maximum.Load(), len(jobs), len(workers))
	}
}

func TestRunJudgeWorkersDrainsInFlightJob(t *testing.T) {
	claimed := testClaimedSubmission()
	queue := &singleJobQueue{job: redisx.JudgeJob{EntryID: "1-0", SubmissionID: claimed.SubmissionID}}
	store := &fakeSubmissionStore{
		claim: claimResult{Kind: claimAcquired, Claimed: claimed},
		completion: completionResult{
			Kind:      completionApplied,
			Completed: testCompletedSubmission(),
		},
	}
	started := make(chan struct{})
	checkContext := make(chan chan error)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseWorker()
	service := newTestJudgeService(queue, store, executorFunc(func(ctx context.Context, _ ExecutionRequest) (ExecutionOutcome, error) {
		close(started)
		select {
		case response := <-checkContext:
			response <- ctx.Err()
		case <-ctx.Done():
			return ExecutionOutcome{}, ctx.Err()
		}
		select {
		case <-release:
			return ExecutionOutcome{Kind: OutcomeWrongAnswer, TestsPassed: 2}, nil
		case <-ctx.Done():
			return ExecutionOutcome{}, ctx.Err()
		}
	}), func(context.Context, string, []byte) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runJudgeWorkers(ctx, []*judgeService{service}, time.Second)
	}()
	receiveJudgeTest(t, started, "draining executor start")
	cancel()

	contextResult := make(chan error, 1)
	sendJudgeTest(t, checkContext, contextResult, "work context check")
	if err := receiveJudgeTest(t, contextResult, "work context result"); err != nil {
		t.Fatalf("work context canceled before shutdown grace elapsed: %v", err)
	}
	releaseWorker()
	if err := receiveJudgeTest(t, done, "draining workers stop"); !errors.Is(err, context.Canceled) {
		t.Fatalf("runJudgeWorkers error = %v, want context cancellation", err)
	}
	if store.completeCalls != 1 {
		t.Fatalf("complete calls = %d, want 1", store.completeCalls)
	}
	if operations := queue.Operations(); !equalStrings(operations, []string{"finalize:1-0"}) {
		t.Fatalf("queue operations = %v", operations)
	}
}

func TestRunJudgeWorkersForceCancelsAfterGrace(t *testing.T) {
	claimed := testClaimedSubmission()
	queue := &singleJobQueue{job: redisx.JudgeJob{EntryID: "1-0", SubmissionID: claimed.SubmissionID}}
	store := &fakeSubmissionStore{claim: claimResult{Kind: claimAcquired, Claimed: claimed}}
	started := make(chan struct{})
	forced := make(chan struct{})
	var publishCalled atomic.Bool
	service := newTestJudgeService(queue, store, executorFunc(func(ctx context.Context, _ ExecutionRequest) (ExecutionOutcome, error) {
		close(started)
		<-ctx.Done()
		close(forced)
		return ExecutionOutcome{}, ctx.Err()
	}), func(context.Context, string, []byte) error {
		publishCalled.Store(true)
		return errors.New("unexpected publish")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runJudgeWorkers(ctx, []*judgeService{service}, 10*time.Millisecond)
	}()
	receiveJudgeTest(t, started, "force-canceled executor start")
	cancel()
	receiveJudgeTest(t, forced, "forced work cancellation")
	if err := receiveJudgeTest(t, done, "force-canceled workers stop"); !errors.Is(err, context.Canceled) {
		t.Fatalf("runJudgeWorkers error = %v, want context cancellation", err)
	}
	if publishCalled.Load() || store.completeCalls != 0 || len(queue.Operations()) != 0 {
		t.Fatalf("completion calls = %d, queue operations = %v", store.completeCalls, queue.Operations())
	}
}

type singleJobQueue struct {
	mu         sync.Mutex
	job        redisx.JudgeJob
	delivered  bool
	operations []string
}

type multiJobQueue struct {
	mu         sync.Mutex
	jobs       []redisx.JudgeJob
	deleted    int
	total      int
	allDeleted chan struct{}
}

func newMultiJobQueue(jobs []redisx.JudgeJob) *multiJobQueue {
	return &multiJobQueue{jobs: append([]redisx.JudgeJob(nil), jobs...), total: len(jobs), allDeleted: make(chan struct{})}
}

func (q *multiJobQueue) Read(ctx context.Context, _ string, _ int64, _ time.Duration) ([]redisx.JudgeJob, error) {
	q.mu.Lock()
	if len(q.jobs) > 0 {
		job := q.jobs[0]
		q.jobs = q.jobs[1:]
		q.mu.Unlock()
		return []redisx.JudgeJob{job}, nil
	}
	q.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (q *multiJobQueue) Finalize(context.Context, string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.deleted++
	if q.deleted == q.total {
		close(q.allDeleted)
	}
	return nil
}

type concurrentTestStore struct{}

func (concurrentTestStore) Claim(
	_ context.Context,
	submissionID, attemptToken uuid.UUID,
	_ time.Duration,
) (claimResult, error) {
	claimed := testClaimedSubmission()
	claimed.SubmissionID = submissionID
	claimed.AttemptToken = attemptToken
	return claimResult{Kind: claimAcquired, Claimed: claimed}, nil
}

func (concurrentTestStore) Complete(
	_ context.Context,
	claimed claimedSubmission,
	result terminalResult,
) (completionResult, error) {
	completed := testCompletedSubmission()
	completed.SubmissionID = claimed.SubmissionID
	completed.Verdict = result.Verdict
	completed.FailureKind = result.FailureKind
	completed.TestsPassed = result.TestsPassed
	return completionResult{Kind: completionApplied, Completed: completed}, nil
}

func (q *singleJobQueue) Read(ctx context.Context, _ string, _ int64, _ time.Duration) ([]redisx.JudgeJob, error) {
	q.mu.Lock()
	if !q.delivered {
		q.delivered = true
		job := q.job
		q.mu.Unlock()
		return []redisx.JudgeJob{job}, nil
	}
	q.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (q *singleJobQueue) Finalize(_ context.Context, entryID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.operations = append(q.operations, "finalize:"+entryID)
	return nil
}

func (q *singleJobQueue) Operations() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.operations...)
}

func receiveJudgeTest[T any](t *testing.T, channel <-chan T, operation string) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", operation)
		var zero T
		return zero
	}
}

func sendJudgeTest[T any](t *testing.T, channel chan<- T, value T, operation string) {
	t.Helper()
	select {
	case channel <- value:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting to send %s", operation)
	}
}
