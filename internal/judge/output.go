package judge

import (
	"bytes"
	"errors"
	"sync"
)

var errOutputLimit = errors.New("sandbox output limit exceeded")

func normalizeOutput(output []byte) []byte {
	normalized := bytes.ReplaceAll(output, []byte("\r\n"), []byte("\n"))
	return bytes.TrimSuffix(normalized, []byte("\n"))
}

func outputMatches(actual, expected []byte) bool {
	return bytes.Equal(normalizeOutput(actual), normalizeOutput(expected))
}

type outputBudget struct {
	mu        sync.Mutex
	remaining int64
}

func newOutputBudget(limit int64) *outputBudget {
	return &outputBudget{remaining: limit}
}

type budgetWriter struct {
	budget *outputBudget
	buffer *bytes.Buffer
}

func (w budgetWriter) Write(p []byte) (int, error) {
	w.budget.mu.Lock()
	defer w.budget.mu.Unlock()
	if int64(len(p)) <= w.budget.remaining {
		w.budget.remaining -= int64(len(p))
		return w.buffer.Write(p)
	}
	allowed := max(w.budget.remaining, 0)
	n, _ := w.buffer.Write(p[:allowed])
	w.budget.remaining = 0
	return n, errOutputLimit
}
