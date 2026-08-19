package gateway

import (
	"sync"

	"github.com/google/uuid"
)

// Registry tracks the single active socket this gateway instance owns per user.
// It holds no game state and is not shared across instances.
type Registry struct {
	mu     sync.RWMutex
	conns  map[uuid.UUID]*conn
	serve  sync.WaitGroup
	closed bool
}

func NewRegistry() *Registry {
	return &Registry{conns: make(map[uuid.UUID]*conn)}
}

func (r *Registry) Add(c *conn) bool {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		c.close()
		return false
	}
	if !c.registered {
		c.registered = true
		r.serve.Add(1)
	}
	prev := r.conns[c.userID]
	r.conns[c.userID] = c
	r.mu.Unlock()
	if prev != nil && prev != c {
		prev.close()
	}
	return true
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

func (r *Registry) CloseAll() {
	r.mu.Lock()
	r.closed = true
	conns := make([]*conn, 0, len(r.conns))
	for _, c := range r.conns {
		conns = append(conns, c)
	}
	r.mu.Unlock()
	for _, c := range conns {
		c.close()
	}
}

func (r *Registry) Serve(c *conn) {
	c.serve()
}

func (r *Registry) Done(c *conn) {
	r.mu.Lock()
	registered := c.registered
	c.registered = false
	r.mu.Unlock()
	if registered {
		r.serve.Done()
	}
}

func (r *Registry) Wait() {
	r.serve.Wait()
}
