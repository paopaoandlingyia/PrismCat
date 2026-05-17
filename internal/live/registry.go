package live

import (
	"sync"

	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

type EventType string

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
			log:  cloneRequestLog(logEntry),
			subs: make(map[int]chan Event),
		}
		return
	}

	current.log = cloneRequestLog(logEntry)
	r.broadcastLocked(current, Event{
		Type: EventSnapshot,
		Log:  cloneRequestLog(current.log),
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

	next := cloneRequestLog(current.log)
	fn(next)
	current.log = next

	r.broadcastLocked(current, Event{
		Type: EventSnapshot,
		Log:  cloneRequestLog(next),
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

	next := cloneRequestLog(current.log)
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

	finalLog := cloneRequestLog(logEntry)
	r.broadcastLocked(current, Event{
		Type: EventCompleted,
		Log:  cloneRequestLog(finalLog),
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
	return cloneRequestLog(current.log), true
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
			Log:  cloneRequestLog(current.log),
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

func cloneRequestLog(in *storage.RequestLog) *storage.RequestLog {
	if in == nil {
		return nil
	}

	out := *in
	out.RequestHeaders = cloneHeaders(in.RequestHeaders)
	out.ResponseHeaders = cloneHeaders(in.ResponseHeaders)
	out.RequestBodyRaw = cloneBytes(in.RequestBodyRaw)
	out.ResponseBodyRaw = cloneBytes(in.ResponseBodyRaw)
	out.UsageInputTokens = cloneInt64Ptr(in.UsageInputTokens)
	out.UsageOutputTokens = cloneInt64Ptr(in.UsageOutputTokens)
	out.UsageTotalTokens = cloneInt64Ptr(in.UsageTotalTokens)
	return &out
}

func cloneHeaders(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string][]string, len(in))
	for k, vv := range in {
		if vv == nil {
			out[k] = nil
			continue
		}
		next := make([]string, len(vv))
		copy(next, vv)
		out[k] = next
	}
	return out
}

func cloneBytes(in []byte) []byte {
	if len(in) == 0 {
		return nil
	}

	out := make([]byte, len(in))
	copy(out, in)
	return out
}

func cloneInt64Ptr(in *int64) *int64 {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
