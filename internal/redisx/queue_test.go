package redisx

import (
	"context"
	"testing"
)

func TestRequeueNoEntriesIsNoop(t *testing.T) {
	var queue *Queue
	if err := queue.Requeue(context.Background()); err != nil {
		t.Fatalf("Requeue with no entries: %v", err)
	}
}
