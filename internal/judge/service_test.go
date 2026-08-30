package judge

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

func TestProcessJobClaimHandling(t *testing.T) {
	completed := testCompletedSubmission()
	claimed := testClaimedSubmission()
	tests := []struct {
		name         string
		job          redisx.JudgeJob
		claim        claimResult
		wantClaim    int
		wantExecute  int
		wantComplete int
		wantPublish  int
		wantQueueOps []string
	}{
		{
			name:         "malformed",
			job:          redisx.JudgeJob{EntryID: "1-0", DecodeErr: errors.New("bad payload")},
			wantQueueOps: []string{"finalize:1-0"},
		},
		{
			name:         "missing",
			job:          redisx.JudgeJob{EntryID: "2-0", SubmissionID: claimed.SubmissionID},
			claim:        claimResult{Kind: claimMissing},
			wantClaim:    1,
			wantQueueOps: []string{"finalize:2-0"},
		},
		{
			name:         "live running",
			job:          redisx.JudgeJob{EntryID: "3-0", SubmissionID: claimed.SubmissionID},
			claim:        claimResult{Kind: claimRunning, LeaseUntil: time.Unix(100, 0)},
			wantClaim:    1,
			wantQueueOps: []string{"finalize:3-0"},
		},
		{
			name:      "expired",
			job:       redisx.JudgeJob{EntryID: "4-0", SubmissionID: claimed.SubmissionID},
			claim:     claimResult{Kind: claimExpired},
			wantClaim: 1,
		},
		{
			name:         "completed duplicate",
			job:          redisx.JudgeJob{EntryID: "5-0", SubmissionID: claimed.SubmissionID},
			claim:        claimResult{Kind: claimCompleted, Completed: completed},
			wantClaim:    1,
			wantPublish:  1,
			wantQueueOps: []string{"finalize:5-0"},
		},
		{
			name:         "acquired",
			job:          redisx.JudgeJob{EntryID: "6-0", SubmissionID: claimed.SubmissionID},
			claim:        claimResult{Kind: claimAcquired, Claimed: claimed},
			wantClaim:    1,
			wantExecute:  1,
			wantComplete: 1,
			wantPublish:  1,
			wantQueueOps: []string{"finalize:6-0"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queue := &fakeJudgeQueue{}
			store := &fakeSubmissionStore{
				claim: test.claim,
				completion: completionResult{
					Kind:      completionApplied,
					Completed: completed,
				},
			}
			executeCalls := 0
			publishCalls := 0
			service := newTestJudgeService(queue, store, executorFunc(func(_ context.Context, request ExecutionRequest) (ExecutionOutcome, error) {
				executeCalls++
				if request.Language != claimed.Language || string(request.Source) != string(claimed.Source) || len(request.Tests) != len(claimed.Tests) {
					t.Fatalf("execution request = %#v", request)
				}
				return ExecutionOutcome{Kind: OutcomeWrongAnswer, TestsPassed: 2}, nil
			}), func(context.Context, string, []byte) error {
				publishCalls++
				return nil
			})

			if err := service.processJob(context.Background(), test.job); err != nil {
				t.Fatalf("processJob: %v", err)
			}
			if store.claimCalls != test.wantClaim || executeCalls != test.wantExecute ||
				store.completeCalls != test.wantComplete || publishCalls != test.wantPublish {
				t.Fatalf("calls claim/execute/complete/publish = %d/%d/%d/%d, want %d/%d/%d/%d",
					store.claimCalls, executeCalls, store.completeCalls, publishCalls,
					test.wantClaim, test.wantExecute, test.wantComplete, test.wantPublish)
			}
			if !equalStrings(queue.operations, test.wantQueueOps) {
				t.Fatalf("queue operations = %v, want %v", queue.operations, test.wantQueueOps)
			}
			if test.wantComplete == 1 {
				want := terminalResult{Verdict: proto.VerdictFail, FailureKind: "wrong_answer", TestsPassed: 2}
				if store.terminal != want {
					t.Fatalf("terminal result = %#v, want %#v", store.terminal, want)
				}
			}
		})
	}
}

func TestProcessJobExecutorErrorDoesNotAck(t *testing.T) {
	queue := &fakeJudgeQueue{}
	store := &fakeSubmissionStore{claim: claimResult{Kind: claimAcquired, Claimed: testClaimedSubmission()}}
	service := newTestJudgeService(queue, store, executorFunc(func(context.Context, ExecutionRequest) (ExecutionOutcome, error) {
		return ExecutionOutcome{}, errors.New("executor unavailable")
	}), func(context.Context, string, []byte) error {
		t.Fatal("publish called")
		return nil
	})

	err := service.processJob(context.Background(), redisx.JudgeJob{EntryID: "1-0", SubmissionID: testClaimedSubmission().SubmissionID})
	if err == nil {
		t.Fatal("processJob returned nil error")
	}
	if store.completeCalls != 0 || len(queue.operations) != 0 {
		t.Fatalf("complete calls = %d, queue operations = %v", store.completeCalls, queue.operations)
	}
}

func TestProcessJobLostCompletionOwnershipDoesNotPublishOrAck(t *testing.T) {
	claimed := testClaimedSubmission()
	queue := &fakeJudgeQueue{}
	store := &fakeSubmissionStore{
		claim:      claimResult{Kind: claimAcquired, Claimed: claimed},
		completion: completionResult{Kind: completionLostOwnership},
	}
	service := newTestJudgeService(queue, store, executorFunc(func(context.Context, ExecutionRequest) (ExecutionOutcome, error) {
		return ExecutionOutcome{Kind: OutcomeWrongAnswer, TestsPassed: 1}, nil
	}), func(context.Context, string, []byte) error {
		t.Fatal("publish called")
		return nil
	})

	err := service.processJob(context.Background(), redisx.JudgeJob{EntryID: "1-0", SubmissionID: claimed.SubmissionID})
	if !errors.Is(err, errCompletionOwnershipLost) {
		t.Fatalf("processJob error = %v, want %v", err, errCompletionOwnershipLost)
	}
	if len(queue.operations) != 0 {
		t.Fatalf("queue operations = %v, want none", queue.operations)
	}
}

func TestProcessJobCompletionErrorDoesNotPublishOrAck(t *testing.T) {
	claimed := testClaimedSubmission()
	queue := &fakeJudgeQueue{}
	completionErr := errors.New("completion transaction failed")
	store := &fakeSubmissionStore{
		claim:       claimResult{Kind: claimAcquired, Claimed: claimed},
		completeErr: completionErr,
	}
	service := newTestJudgeService(queue, store, executorFunc(func(context.Context, ExecutionRequest) (ExecutionOutcome, error) {
		return ExecutionOutcome{Kind: OutcomeWrongAnswer, TestsPassed: 1}, nil
	}), func(context.Context, string, []byte) error {
		t.Fatal("publish called")
		return nil
	})

	err := service.processJob(context.Background(), redisx.JudgeJob{EntryID: "1-0", SubmissionID: claimed.SubmissionID})
	if !errors.Is(err, completionErr) {
		t.Fatalf("processJob error = %v, want %v", err, completionErr)
	}
	if len(queue.operations) != 0 {
		t.Fatalf("queue operations = %v, want none", queue.operations)
	}
}

func TestPublishAndFinalizePublishesAllBeforeFinalizing(t *testing.T) {
	queue := &fakeJudgeQueue{}
	completed := testCompletedSubmission()
	completed.WinnerID = completed.Players[0]
	var channels []string
	service := newTestJudgeService(queue, &fakeSubmissionStore{}, executorFunc(nil), func(_ context.Context, channel string, _ []byte) error {
		channels = append(channels, channel)
		if len(channels) == 1 {
			return errors.New("publish failed")
		}
		return nil
	})

	if err := service.publishAndFinalize(context.Background(), "1-0", completed); err == nil {
		t.Fatal("publishAndFinalize returned nil error")
	}
	wantChannels := []string{redisx.UserChannel(completed.Players[0]), redisx.UserChannel(completed.Players[1])}
	if !equalStrings(channels, wantChannels) {
		t.Fatalf("published channels = %v, want %v", channels, wantChannels)
	}
	if len(queue.operations) != 0 {
		t.Fatalf("queue operations = %v, want none", queue.operations)
	}
}

func TestFinalizeJobAtomically(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		queue := &fakeJudgeQueue{}
		service := newTestJudgeService(queue, &fakeSubmissionStore{}, executorFunc(nil), func(context.Context, string, []byte) error { return nil })
		if err := service.finalizeJob(context.Background(), "1-0"); err != nil {
			t.Fatalf("finalizeJob: %v", err)
		}
		want := []string{"finalize:1-0"}
		if !equalStrings(queue.operations, want) {
			t.Fatalf("operations = %v, want %v", queue.operations, want)
		}
	})

	t.Run("failure", func(t *testing.T) {
		finalizeErr := errors.New("finalize failed")
		queue := &fakeJudgeQueue{finalizeErr: finalizeErr}
		service := newTestJudgeService(queue, &fakeSubmissionStore{}, executorFunc(nil), func(context.Context, string, []byte) error { return nil })
		if err := service.finalizeJob(context.Background(), "2-0"); !errors.Is(err, finalizeErr) {
			t.Fatalf("finalizeJob error = %v, want %v", err, finalizeErr)
		}
		want := []string{"finalize:2-0"}
		if !equalStrings(queue.operations, want) {
			t.Fatalf("operations = %v, want %v", queue.operations, want)
		}
	})
}

type fakeJudgeQueue struct {
	operations  []string
	finalizeErr error
}

func (*fakeJudgeQueue) Read(context.Context, string, int64, time.Duration) ([]redisx.JudgeJob, error) {
	return nil, nil
}

func (q *fakeJudgeQueue) Finalize(_ context.Context, entryID string) error {
	q.operations = append(q.operations, "finalize:"+entryID)
	return q.finalizeErr
}

type fakeSubmissionStore struct {
	claim         claimResult
	claimErr      error
	completion    completionResult
	completeErr   error
	claimCalls    int
	completeCalls int
	terminal      terminalResult
}

func (s *fakeSubmissionStore) Claim(context.Context, uuid.UUID, uuid.UUID, time.Duration) (claimResult, error) {
	s.claimCalls++
	return s.claim, s.claimErr
}

func (s *fakeSubmissionStore) Complete(_ context.Context, _ claimedSubmission, terminal terminalResult) (completionResult, error) {
	s.completeCalls++
	s.terminal = terminal
	return s.completion, s.completeErr
}

type executorFunc func(context.Context, ExecutionRequest) (ExecutionOutcome, error)

func (f executorFunc) Execute(ctx context.Context, request ExecutionRequest) (ExecutionOutcome, error) {
	return f(ctx, request)
}

func newTestJudgeService(
	queue judgeQueue,
	store submissionStore,
	executor Executor,
	publish func(context.Context, string, []byte) error,
) *judgeService {
	return &judgeService{
		logger:   slog.New(slog.DiscardHandler),
		queue:    queue,
		store:    store,
		executor: executor,
		publish:  publish,
		consumer: "test-consumer",
		lease:    time.Minute,
		limits:   testLimits(),
	}
}

func testClaimedSubmission() claimedSubmission {
	completed := testCompletedSubmission()
	return claimedSubmission{
		SubmissionID: completed.SubmissionID,
		MatchID:      completed.MatchID,
		PlayerID:     completed.PlayerID,
		ProblemID:    uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		Language:     LanguagePython,
		Source:       []byte("print('ok')"),
		Tests: []TestCase{
			{Expected: []byte("one")},
			{Expected: []byte("two")},
			{Expected: []byte("three")},
		},
		Players:      completed.Players,
		AttemptToken: uuid.MustParse("66666666-6666-6666-6666-666666666666"),
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
