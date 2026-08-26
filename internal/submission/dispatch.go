package submission

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultDispatchBatchSize = 32

type immediateDispatcher interface {
	Dispatch(context.Context, uuid.UUID) error
}

type JobEnqueuer interface {
	Enqueue(context.Context, uuid.UUID) (string, error)
}

// Dispatcher bridges durable pending submissions to the Redis Judge Stream.
type Dispatcher struct {
	db         *pgxpool.Pool
	queue      JobEnqueuer
	staleAfter time.Duration
	batchSize  int
}

func NewDispatcher(db *pgxpool.Pool, queue JobEnqueuer, staleAfter time.Duration, batchSize int) (*Dispatcher, error) {
	if db == nil || queue == nil {
		return nil, errors.New("initialize submission dispatcher: missing dependency")
	}
	if staleAfter <= 0 {
		return nil, errors.New("initialize submission dispatcher: stale interval must be positive")
	}
	if batchSize <= 0 {
		return nil, errors.New("initialize submission dispatcher: batch size must be positive")
	}
	return &Dispatcher{db: db, queue: queue, staleAfter: staleAfter, batchSize: batchSize}, nil
}

// Dispatch immediately enqueues one durable submission and marks it only after XADD.
func (d *Dispatcher) Dispatch(ctx context.Context, submissionID uuid.UUID) error {
	if d == nil || d.db == nil || d.queue == nil {
		return errors.New("dispatch submission: missing dependency")
	}
	if submissionID == uuid.Nil {
		return errors.New("dispatch submission: missing submission ID")
	}
	if _, err := d.queue.Enqueue(ctx, submissionID); err != nil {
		return fmt.Errorf("dispatch submission: enqueue judge job: %w", err)
	}
	if _, err := d.db.Exec(ctx, `
		UPDATE submissions
		SET last_enqueued_at = clock_timestamp()
		WHERE id = $1 AND status = 'pending'
	`, submissionID); err != nil {
		return fmt.Errorf("dispatch submission: mark enqueued: %w", err)
	}
	return nil
}

// DispatchPending recovers pending rows that have never been enqueued or are stale.
// The transaction deliberately spans XADD so competing Match replicas skip its rows.
func (d *Dispatcher) DispatchPending(ctx context.Context) (int, error) {
	if d == nil || d.db == nil || d.queue == nil {
		return 0, errors.New("dispatch pending submissions: missing dependency")
	}
	tx, err := d.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("dispatch pending submissions: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id
		FROM submissions
		WHERE status = 'pending'
		  AND (
			last_enqueued_at IS NULL
			OR last_enqueued_at <= clock_timestamp() - ($1 * interval '1 microsecond')
		  )
		ORDER BY created_at, id
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, d.staleAfter.Microseconds(), d.batchSize)
	if err != nil {
		return 0, fmt.Errorf("dispatch pending submissions: select candidates: %w", err)
	}
	ids := make([]uuid.UUID, 0, d.batchSize)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("dispatch pending submissions: scan candidate: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("dispatch pending submissions: iterate candidates: %w", err)
	}
	rows.Close()

	for _, id := range ids {
		if _, err := d.queue.Enqueue(ctx, id); err != nil {
			return 0, fmt.Errorf("dispatch pending submissions: enqueue judge job: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE submissions
			SET last_enqueued_at = clock_timestamp()
			WHERE id = $1 AND status = 'pending'
		`, id); err != nil {
			return 0, fmt.Errorf("dispatch pending submissions: mark enqueued: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("dispatch pending submissions: commit transaction: %w", err)
	}
	return len(ids), nil
}
