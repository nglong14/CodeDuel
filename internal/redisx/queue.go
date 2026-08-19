package redisx

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const DefaultScanLimit = 100

var (
	//go:embed scripts/enqueue.lua
	enqueueSource string
	//go:embed scripts/pop_pair.lua
	popPairSource string
	//go:embed scripts/requeue.lua
	requeueSource string

	enqueueScript = redis.NewScript(enqueueSource)
	popPairScript = redis.NewScript(popPairSource)
	requeueScript = redis.NewScript(requeueSource)
)

type Queue struct {
	client    redis.Scripter
	scanLimit int
}

type EnqueueResult struct {
	Added bool
	Score int64
}

type QueueEntry struct {
	Member  QueueMember
	Score   int64
	encoded string
}

type Pair [2]QueueEntry

func NewQueue(client redis.Scripter, scanLimit int) *Queue {
	if scanLimit <= 0 {
		scanLimit = DefaultScanLimit
	}
	return &Queue{client: client, scanLimit: scanLimit}
}

func (q *Queue) Enqueue(ctx context.Context, member QueueMember) (EnqueueResult, error) {
	if q == nil || q.client == nil {
		return EnqueueResult{}, fmt.Errorf("enqueue member: missing Redis client")
	}
	encoded, err := encodeMember(member)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("enqueue member: %w", err)
	}

	result, err := enqueueScript.Run(ctx, q.client,
		[]string{QueueKey, MembersKey, LastScoreKey}, member.UserID.String(), encoded).Result()
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("enqueue member: run script: %w", err)
	}
	values, err := scriptValues(result, 2)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("enqueue member: %w", err)
	}
	added, err := strconv.ParseBool(values[0])
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("enqueue member: decode added flag: %w", err)
	}
	score, err := strconv.ParseInt(values[1], 10, 64)
	if err != nil {
		return EnqueueResult{}, fmt.Errorf("enqueue member: decode score: %w", err)
	}
	return EnqueueResult{Added: added, Score: score}, nil
}

func (q *Queue) PopPair(ctx context.Context) (*Pair, error) {
	if q == nil || q.client == nil {
		return nil, fmt.Errorf("pop pair: missing Redis client")
	}
	result, err := popPairScript.Run(ctx, q.client,
		[]string{QueueKey, MembersKey}, q.scanLimit).Result()
	if err != nil {
		return nil, fmt.Errorf("pop pair: run script: %w", err)
	}
	values, err := scriptValues(result, -1)
	if err != nil {
		return nil, fmt.Errorf("pop pair: %w", err)
	}
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) != 4 {
		return nil, fmt.Errorf("pop pair: unexpected result length %d", len(values))
	}

	var pair Pair
	for i := range pair {
		encoded := values[i*2]
		member, decodeErr := decodeMember(encoded)
		if decodeErr != nil {
			return nil, fmt.Errorf("pop pair: decode member %d: %w", i+1, decodeErr)
		}
		score, parseErr := strconv.ParseInt(values[i*2+1], 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("pop pair: decode score %d: %w", i+1, parseErr)
		}
		pair[i] = QueueEntry{Member: member, Score: score, encoded: encoded}
	}
	if pair[0].Member.UserID == pair[1].Member.UserID {
		return nil, fmt.Errorf("pop pair: duplicate user")
	}
	return &pair, nil
}

func (q *Queue) Requeue(ctx context.Context, pair Pair) error {
	if q == nil || q.client == nil {
		return fmt.Errorf("requeue pair: missing Redis client")
	}
	args := make([]any, 0, 6)
	for i, entry := range pair {
		if err := entry.Member.Validate(); err != nil {
			return fmt.Errorf("requeue pair: invalid member %d: %w", i+1, err)
		}
		if entry.encoded == "" {
			return fmt.Errorf("requeue pair: member %d has no original encoding", i+1)
		}
		args = append(args, entry.Member.UserID.String(), entry.encoded, entry.Score)
	}
	if _, err := requeueScript.Run(ctx, q.client,
		[]string{QueueKey, MembersKey}, args...).Result(); err != nil {
		return fmt.Errorf("requeue pair: run script: %w", err)
	}
	return nil
}

func scriptValues(result any, expected int) ([]string, error) {
	items, ok := result.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected script result type %T", result)
	}
	if expected >= 0 && len(items) != expected {
		return nil, fmt.Errorf("unexpected script result length %d", len(items))
	}
	values := make([]string, len(items))
	for i, item := range items {
		switch value := item.(type) {
		case string:
			values[i] = value
		case []byte:
			values[i] = string(value)
		default:
			values[i] = fmt.Sprint(value)
		}
	}
	return values, nil
}
