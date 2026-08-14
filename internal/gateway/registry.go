package gateway

import (
	"sync"

	"github.com/google/uuid"
)

// Registry tracks the single active socket this gateway instance owns per user.
// It holds no game state and is not shared across instances.
type Registry struct {
	mu    sync.RWMutex
	conns map[uuid.UUID]*conn
}

func NewRegistry() *Registry {
	return &Registry{conns: make(map[uuid.UUID]*conn)}
}

func (r *Registry) Add(c *conn) {
	r.mu.Lock()
	prev := r.conns[c.userID]
	r.conns[c.userID] = c
	r.mu.Unlock()
	if prev != nil && prev != c {
		prev.close()
	}
}

func (r *Registry) Remove(c *conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.conns[c.userID]; ok && cur == c {
		delete(r.conns, c.userID)
	}
}

func (r *Registry) Get(userID uuid.UUID) *conn {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.conns[userID]
}
