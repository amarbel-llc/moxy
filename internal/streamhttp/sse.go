package streamhttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/jsonrpc"
)

type sseStream struct {
	id    string
	w     http.ResponseWriter
	flush http.Flusher
	done  chan struct{}

	// mu serializes all writes to w/flush. An http.ResponseWriter is not
	// safe for concurrent use, but broadcast() runs on the Server.Notify
	// goroutine while the owning handler goroutine (and net/http's own
	// connection teardown) also touch w — without this lock the chunked
	// framing corrupts ("invalid byte in chunk length" on the client, #343).
	mu sync.Mutex
	// closed flips true once the owning handler is tearing down, so a
	// concurrent broadcast can't write to a response that net/http is
	// finishing. Guarded by mu; close of done happens exactly once.
	closed bool
}

// writeEvent writes one SSE event and flushes, serialized against other
// writers and against teardown. A no-op once the stream is closed.
func (s *sseStream) writeEvent(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	fmt.Fprint(s.w, event)
	s.flush.Flush()
}

// closeStream marks the stream closed and closes done exactly once. Taking
// mu means it waits for any in-flight writeEvent to finish, so the owning
// handler never returns (triggering net/http's finishRequest) while a
// broadcast is mid-write.
func (s *sseStream) closeStream() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.done)
}

type streamRegistry struct {
	streams map[string]*sseStream
	mu      sync.RWMutex
}

func newStreamRegistry() *streamRegistry {
	return &streamRegistry{
		streams: make(map[string]*sseStream),
	}
}

func (r *streamRegistry) add(s *sseStream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.streams[s.id] = s
}

func (r *streamRegistry) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.streams, id)
}

func (r *streamRegistry) closeAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.streams {
		s.closeStream()
	}
	r.streams = make(map[string]*sseStream)
}

func (r *streamRegistry) broadcast(msg *jsonrpc.Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	event := fmt.Sprintf("data: %s\n\n", data)

	r.mu.RLock()
	snapshot := make([]*sseStream, 0, len(r.streams))
	for _, s := range r.streams {
		snapshot = append(snapshot, s)
	}
	r.mu.RUnlock()

	for _, s := range snapshot {
		s.writeEvent(event)
	}
}
