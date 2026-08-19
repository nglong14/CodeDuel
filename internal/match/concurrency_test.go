package match

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nglong14/CodeDuel/internal/redisx"
)

func TestServiceConcurrentStepsUseQueueAtomicity(t *testing.T) {
	// The service owns no shared durable state; this test keeps its local collaborators race-safe.
	pair := testPair()
	queue := &lockedFakeQueue{pair: &pair}
	service := mustService(t, queue,
		func(context.Context, [2]redisx.QueueMember) (CreatedMatch, error) {
			return CreatedMatch{MatchID: uuid.New(), ProblemID: uuid.New(), Deadline: time.Now()}, nil
		},
		func(context.Context, string, []byte) error { return nil },
	)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			service.step(context.Background())
		}()
	}
	wg.Wait()
}

type lockedFakeQueue struct {
	mu   sync.Mutex
	pair *redisx.Pair
}

func (q *lockedFakeQueue) PopPair(context.Context) (*redisx.Pair, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	pair := q.pair
	q.pair = nil
	return pair, nil
}

func (*lockedFakeQueue) Requeue(context.Context, redisx.Pair) error { return nil }
