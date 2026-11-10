package shadow

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// closeWait bounds Close: a log write that never returns must not hold up
// shutdown.
const closeWait = 2 * time.Second

type pending struct {
	name string
	args []byte
}

// Async runs an Observer off the dispatch path. Observe copies the arguments
// and offers them to a bounded queue without ever blocking; when the queue is
// full (a slow or stuck log) the call is dropped and counted, so shadow mode
// costs a dispatch one allocation and a channel send at most.
type Async struct {
	obs     *Observer
	ch      chan pending
	done    chan struct{}
	mu      sync.RWMutex
	closed  bool
	dropped atomic.Uint64
}

// NewAsync starts the worker. queue is the number of calls held before
// dropping begins (minimum 1).
func NewAsync(o *Observer, queue int) *Async {
	if queue < 1 {
		queue = 1
	}
	a := &Async{obs: o, ch: make(chan pending, queue), done: make(chan struct{})}
	go a.run()
	return a
}

func (a *Async) run() {
	defer close(a.done)
	for p := range a.ch {
		a.handle(p)
	}
}

func (a *Async) handle(p pending) {
	defer func() { _ = recover() }() // a bad call never kills the worker
	var args map[string]any
	if len(p.args) > 0 {
		if err := json.Unmarshal(p.args, &args); err != nil {
			a.obs.write(Record{Tool: p.name, Verdict: VerdictRefused, Error: "arguments are not a JSON object"})
			return
		}
	}
	a.obs.Observe(p.name, args)
}

// Observe queues one dispatched call. It never blocks, never panics and
// returns nothing.
func (a *Async) Observe(name string, args json.RawMessage) {
	if a == nil {
		return
	}
	defer func() { _ = recover() }()
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		return
	}
	cp := append([]byte(nil), args...)
	select {
	case a.ch <- pending{name: name, args: cp}:
	default:
		a.dropped.Add(1)
	}
}

// Dropped is how many calls were dropped because the queue was full.
func (a *Async) Dropped() uint64 { return a.dropped.Load() }

// Close stops accepting calls and waits, bounded, for the queue to drain.
func (a *Async) Close() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		close(a.ch)
	}
	a.mu.Unlock()
	select {
	case <-a.done:
	case <-time.After(closeWait):
	}
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// OpenLog opens (creating) the shadow log for a session under
// <dir>/shadow, mode 0600 in a 0700 directory. dir is moxy's log directory.
func OpenLog(dir, session string) (*os.File, error) {
	d := filepath.Join(dir, "shadow")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return nil, err
	}
	name := unsafeName.ReplaceAllString(session, "_")
	if name == "" {
		name = "no-session"
	}
	f, err := os.OpenFile(filepath.Join(d, name+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

var scpRemote = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

// ParseRemote reads owner/name and host out of a git remote URL, the way an
// omitted smith repo/host is resolved (catalog resolve "cwd-remote").
func ParseRemote(remote string) (repo, host string, ok bool) {
	remote = strings.TrimSpace(remote)
	var path string
	switch {
	case strings.Contains(remote, "://"):
		u, err := url.Parse(remote)
		if err != nil || u.Hostname() == "" {
			return "", "", false
		}
		host, path = u.Hostname(), u.Path
	default:
		m := scpRemote.FindStringSubmatch(remote)
		if m == nil {
			return "", "", false
		}
		host, path = m[1], m[2]
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || strings.Count(path, "/") != 1 {
		return "", "", false
	}
	return path, host, true
}
