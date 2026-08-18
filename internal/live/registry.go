package live

import (
	"sync"

	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

type EventType string

const DefaultPreviewLimit int64 = 64 << 10

const (
	EventSnapshot      EventType = "snapshot"
	EventResponseChunk EventType = "response_chunk"
	EventCompleted     EventType = "completed"
)

type Event struct {
	Type      EventType           `json:"type"`
	Log       *storage.RequestLog `json:"log,omitempty"`
	Chunk     string              `json:"chunk,omitempty"`
	SizeDelta int64               `json:"size_delta,omitempty"`
}

type Registry struct {
	mu                   sync.RWMutex
	responsePreviewLimit int64
	entries              map[string]*entry
}

type entry struct {
	log       *storage.RequestLog
	subs      map[int]chan Event
	nextSubID int
}

func NewRegistry(responsePreviewLimit int64) *Registry {
	return &Registry{
		responsePreviewLimit: responsePreviewLimit,
		entries:              make(map[string]*entry),
	}
}
func (r *Registry) Register(logEntry *storage.RequestLog) {
	if logEntry == nil || logEntry.ID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.entries[logEntry.ID]
	if !ok {
		r.entries[logEntry.ID] = &entry{
			log:  logEntry.Clone(),
			subs: make(map[int]chan Event),
		}
		return
	}

	current.log = logEntry.Clone()
	r.broadcastLocked(current, Event{
		Type: EventSnapshot,
		Log:  current.log.Clone(),
	})
}

func (r *Registry) UpdateSnapshot(id string, fn func(*storage.RequestLog)) {
	if id == "" || fn == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.entries[id]
	if !ok {
		return
	}

	next := current.log.Clone()
	fn(next)
	current.log = next

	r.broadcastLocked(current, Event{
		Type: EventSnapshot,
		Log:  next.Clone(),
	})
}

func (r *Registry) AppendResponseChunk(id string, chunk string, sizeDelta int64) {
	if id == "" || sizeDelta <= 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.entries[id]
	if !ok {
		return
	}

	next := current.log.Clone()
	next.ResponseBodySize += sizeDelta

	previewChunk := chunk
	if r.responsePreviewLimit > 0 {
		currentBytes := int64(len(next.ResponseBody))
		remaining := r.responsePreviewLimit - currentBytes
		switch {
		case remaining <= 0:
			previewChunk = ""
			next.Truncated = true
		case int64(len(previewChunk)) > remaining:
			previewChunk = previewChunk[:remaining]
			next.Truncated = true
		}
	}

	if previewChunk != "" {
		next.ResponseBody += previewChunk
	}
	current.log = next

	r.broadcastLocked(current, Event{
		Type:      EventResponseChunk,
		Chunk:     previewChunk,
		SizeDelta: sizeDelta,
	})
}

func (r *Registry) Complete(logEntry *storage.RequestLog) {
	if logEntry == nil || logEntry.ID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.entries[logEntry.ID]
	if !ok {
		return
	}

	finalLog := logEntry.Clone()
	r.broadcastLocked(current, Event{
		Type: EventCompleted,
		Log:  finalLog.Clone(),
	})

	for _, ch := range current.subs {
		close(ch)
	}
	delete(r.entries, logEntry.ID)
}

func (r *Registry) Remove(id string) {
	if id == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.entries[id]
	if !ok {
		return
	}
	for _, ch := range current.subs {
		close(ch)
	}
	delete(r.entries, id)
}

func (r *Registry) Snapshot(id string) (*storage.RequestLog, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	current, ok := r.entries[id]
	if !ok || current.log == nil {
		return nil, false
	}
	return current.log.Clone(), true
}

func (r *Registry) Subscribe(id string) (<-chan Event, func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	current, ok := r.entries[id]
	if !ok {
		return nil, nil, false
	}

	ch := make(chan Event, 128)
	subID := current.nextSubID
	current.nextSubID++
	current.subs[subID] = ch

	if current.log != nil {
		ch <- Event{
			Type: EventSnapshot,
			Log:  current.log.Clone(),
		}
	}

	cancel := func() {
		r.mu.Lock()
		defer r.mu.Unlock()

		entry, ok := r.entries[id]
		if !ok {
			return
		}
		sub, ok := entry.subs[subID]
		if !ok {
			return
		}
		delete(entry.subs, subID)
		close(sub)
	}

	return ch, cancel, true
}

func (r *Registry) broadcastLocked(current *entry, event Event) {
	for _, ch := range current.subs {
		select {
		case ch <- event:
		default:
		}
	}
}
