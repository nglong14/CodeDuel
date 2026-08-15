package gateway

import (
	"testing"

	"github.com/google/uuid"
)

func TestRegistryAddGetRemove(t *testing.T) {
	r := NewRegistry()
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	c := newConn(userID, nil, r)

	r.Add(c)
	if got := r.Get(userID); got != c {
		t.Fatalf("Get = %p, want %p", got, c)
	}

	r.Remove(c)
	if got := r.Get(userID); got != nil {
		t.Fatalf("Get after Remove = %p, want nil", got)
	}
}

func TestRegistryReplaceClosesOldConn(t *testing.T) {
	r := NewRegistry()
	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	old := newConn(userID, nil, r)
	next := newConn(userID, nil, r)

	r.Add(old)
	r.Add(next)

	if got := r.Get(userID); got != next {
		t.Fatalf("Get = %p, want replacement %p", got, next)
	}

	select {
	case <-old.closed:
	default:
		t.Fatal("expected old conn to be closed on replace")
	}

	r.Remove(old)
	if got := r.Get(userID); got != next {
		t.Fatal("Remove of replaced conn deleted the new conn")
	}

	r.Remove(next)
	if got := r.Get(userID); got != nil {
		t.Fatalf("Get after removing current conn = %p, want nil", got)
	}
}

func TestRegistryGetMissing(t *testing.T) {
	r := NewRegistry()
	if got := r.Get(uuid.MustParse("11111111-1111-1111-1111-111111111111")); got != nil {
		t.Fatalf("Get missing = %p, want nil", got)
	}
}

func TestRegistryCloseAll(t *testing.T) {
	r := NewRegistry()
	a := newConn(uuid.MustParse("11111111-1111-1111-1111-111111111111"), nil, r)
	b := newConn(uuid.MustParse("22222222-2222-2222-2222-222222222222"), nil, r)
	r.Add(a)
	r.Add(b)

	r.CloseAll()

	select {
	case <-a.closed:
	default:
		t.Fatal("expected first conn to be closed")
	}
	select {
	case <-b.closed:
	default:
		t.Fatal("expected second conn to be closed")
	}
}
