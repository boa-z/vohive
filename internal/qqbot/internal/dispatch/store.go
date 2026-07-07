package dispatch

import (
	"strings"
	"sync"
	"time"
)

type SeenStore interface {
	Seen(kind string, id string, now time.Time, ttl time.Duration) bool
}

type MemorySeenStore struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func NewMemorySeenStore() *MemorySeenStore {
	return &MemorySeenStore{
		seen: make(map[string]time.Time),
	}
}

func (s *MemorySeenStore) Seen(kind string, id string, now time.Time, ttl time.Duration) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	key := strings.TrimSpace(kind) + ":" + id

	s.mu.Lock()
	defer s.mu.Unlock()

	for existing, deadline := range s.seen {
		if now.After(deadline) {
			delete(s.seen, existing)
		}
	}
	if deadline, ok := s.seen[key]; ok && now.Before(deadline) {
		return true
	}
	s.seen[key] = now.Add(ttl)
	return false
}
