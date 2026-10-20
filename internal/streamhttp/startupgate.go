package streamhttp

import (
	"net/http"
	"sync"
	"time"
)

// StartupGate is the http.Handler a fixed-address serve-http answers with from
// the moment it binds until child servers have finished starting. A tunnel in
// front of moxy therefore finds an origin (not a refused connection, i.e. a
// 502) across a restart.
//
// Until Ready, /healthz answers 503 {"status":"starting"}, and every other
// request is held on its accepted connection, then served by the real handler
// once Ready fires, so the initialize Instructions it sees are complete. A
// request held longer than maxHold gets a well-formed 503 with Retry-After
// rather than hanging into the tunnel's own timeout.
type StartupGate struct {
	maxHold time.Duration
	ready   chan struct{}

	mu   sync.RWMutex
	next http.Handler
	once sync.Once
}

func NewStartupGate(maxHold time.Duration) *StartupGate {
	return &StartupGate{maxHold: maxHold, ready: make(chan struct{})}
}

// Ready installs the real handler and releases every held request. Only the
// first call has effect.
func (g *StartupGate) Ready(next http.Handler) {
	g.once.Do(func() {
		g.mu.Lock()
		g.next = next
		g.mu.Unlock()
		close(g.ready)
	})
}

func (g *StartupGate) handler() http.Handler {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.next
}

func (g *StartupGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if next := g.handler(); next != nil {
		next.ServeHTTP(w, r)
		return
	}

	if r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"starting"}` + "\n"))
		return
	}

	hold := time.NewTimer(g.maxHold)
	defer hold.Stop()
	select {
	case <-g.ready:
		g.handler().ServeHTTP(w, r)
	case <-hold.C:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "moxy: still starting child servers; retry", http.StatusServiceUnavailable)
	case <-r.Context().Done():
	}
}
