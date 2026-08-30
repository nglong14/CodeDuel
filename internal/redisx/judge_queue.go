package redisx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	judgeJobSchemaVersion  = "1"
	maxJudgeReadBlock      = 250 * time.Millisecond
	finalizeJudgeJobSource = `
local acknowledged = redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
if acknowledged == 0 then
    local existing = redis.call('XRANGE', KEYS[1], ARGV[2], ARGV[2])
    if #existing == 0 then
        return 1
    end
    return 0
end
redis.call('XDEL', KEYS[1], ARGV[2])
return 1
`
)

var finalizeJudgeJobScript = redis.NewScript(finalizeJudgeJobSource)

type JudgeJob struct {
	EntryID      string
	SubmissionID uuid.UUID
	DecodeErr    error
}

// JudgeQueue owns the versioned Redis Stream contract for Judge jobs.
type JudgeQueue struct {
	client redis.Cmdable
}

func NewJudgeQueue(client redis.Cmdable) *JudgeQueue {
	return &JudgeQueue{client: client}
}

func (q *JudgeQueue) EnsureGroup(ctx context.Context) error {
	if q == nil || q.client == nil {
		return errors.New("create judge group: missing Redis client")
	}
	err := q.client.XGroupCreateMkStream(ctx, JudgeJobsKey, JudgeConsumerGroup, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create judge group: %w", err)
	}
	return nil
}

// Enqueue adds only the versioned submission ID, keeping PostgreSQL authoritative.
func (q *JudgeQueue) Enqueue(ctx context.Context, submissionID uuid.UUID) (string, error) {
	if q == nil || q.client == nil {
		return "", errors.New("enqueue judge job: missing Redis client")
	}
	if submissionID == uuid.Nil {
		return "", errors.New("enqueue judge job: missing submission ID")
	}
	entryID, err := q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: JudgeJobsKey,
		Values: map[string]any{
			"schema_version": judgeJobSchemaVersion,
			"submission_id":  submissionID.String(),
		},
	}).Result()
	if err != nil {
		return "", fmt.Errorf("enqueue judge job: %w", err)
	}
	return entryID, nil
}

// Read receives only new jobs. Entries with DecodeErr retain their IDs so callers can
// decide whether malformed work should be acknowledged.
func (q *JudgeQueue) Read(ctx context.Context, consumer string, count int64, block time.Duration) ([]JudgeJob, error) {
	if q == nil || q.client == nil {
		return nil, errors.New("read judge jobs: missing Redis client")
	}
	if consumer == "" || count <= 0 || block < 0 {
		return nil, errors.New("read judge jobs: invalid read arguments")
	}
	if block > maxJudgeReadBlock {
		block = maxJudgeReadBlock
	}
	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    JudgeConsumerGroup,
		Consumer: consumer,
		Streams:  []string{JudgeJobsKey, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read judge jobs: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	jobs := make([]JudgeJob, 0)
	for _, stream := range streams {
		if stream.Stream != JudgeJobsKey {
			return nil, fmt.Errorf("read judge jobs: unexpected stream %q", stream.Stream)
		}
		for _, message := range stream.Messages {
			job := decodeJudgeJob(message)
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// Finalize atomically acknowledges and removes one successfully processed entry.
func (q *JudgeQueue) Finalize(ctx context.Context, entryID string) error {
	if q == nil || q.client == nil {
		return errors.New("finalize judge job: missing Redis client")
	}
	if entryID == "" {
		return errors.New("finalize judge job: missing entry ID")
	}
	finalized, err := finalizeJudgeJobScript.Run(
		ctx,
		q.client,
		[]string{JudgeJobsKey},
		JudgeConsumerGroup,
		entryID,
	).Int64()
	if err != nil {
		return fmt.Errorf("finalize judge job: %w", err)
	}
	if finalized != 1 {
		return fmt.Errorf("finalize judge job: entry %q was not pending", entryID)
	}
	return nil
}

func decodeJudgeJob(message redis.XMessage) JudgeJob {
	job := JudgeJob{EntryID: message.ID}
	if len(message.Values) != 2 {
		job.DecodeErr = fmt.Errorf("invalid judge job fields: got %d, want 2", len(message.Values))
		return job
	}
	version, ok := judgeJobField(message.Values, "schema_version")
	if !ok || version != judgeJobSchemaVersion {
		job.DecodeErr = errors.New("invalid judge job schema version")
		return job
	}
	submissionRaw, ok := judgeJobField(message.Values, "submission_id")
	if !ok {
		job.DecodeErr = errors.New("invalid judge job submission ID")
		return job
	}
	submissionID, err := uuid.Parse(submissionRaw)
	if err != nil || submissionID == uuid.Nil {
		job.DecodeErr = errors.New("invalid judge job submission ID")
		return job
	}
	job.SubmissionID = submissionID
	return job
}

func judgeJobField(fields map[string]any, key string) (string, bool) {
	value, ok := fields[key]
	if !ok {
		return "", false
	}
	switch value := value.(type) {
	case string:
		return value, true
	case []byte:
		return string(value), true
	default:
		return "", false
	}
}
