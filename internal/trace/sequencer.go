package trace

import (
	"sync"
	"time"
)

type traceEntry struct {
	seq      int
	lastSeen time.Time
}

type Sequencer struct {
	mu      sync.Mutex
	entries map[string]*traceEntry
}

func NewSequencer() *Sequencer {
	return &Sequencer{
		entries: make(map[string]*traceEntry),
	}
}

func (s *Sequencer) Next(traceID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[traceID]
	if !ok {
		e = &traceEntry{}
		s.entries[traceID] = e
	}
	e.seq++
	e.lastSeen = time.Now()
	return e.seq
}

func (s *Sequencer) Cleanup(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, e := range s.entries {
		if e.lastSeen.Before(cutoff) {
			delete(s.entries, id)
		}
	}
}

func (s *Sequencer) StartCleanup(interval, maxAge time.Duration, stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.Cleanup(maxAge)
			case <-stop:
				return
			}
		}
	}()
}
