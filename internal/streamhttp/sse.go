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
		close(s.done)
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
	defer r.mu.RUnlock()
	for _, s := range r.streams {
		select {
		case <-s.done:
			continue
		default:
		}
		// Best-effort write; if the client is gone the next flush will fail
		// and the stream will be cleaned up by the GET handler.
		fmt.Fprint(s.w, event)
		s.flush.Flush()
	}
}
