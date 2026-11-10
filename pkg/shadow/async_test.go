package shadow

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// syncBuffer is a bytes.Buffer safe for the async goroutine and the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// blockedWriter blocks every Write until released.
type blockedWriter struct{ release chan struct{} }

func (w *blockedWriter) Write(p []byte) (int, error) {
	<-w.release
	return len(p), nil
}

func TestAsyncObserveNeverBlocksOnABlockedLog(t *testing.T) {
	w := &blockedWriter{release: make(chan struct{})}
	defer close(w.release)
	a := NewAsync(NewObserver(newTestBuilder(t, testEnv()), nil, w), 4)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			a.Observe("man.toc", json.RawMessage(`{"page":"ls"}`))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Observe blocked while the log write was blocked")
	}
	if a.Dropped() == 0 {
		t.Error("expected dropped calls with a blocked log and a 4-slot queue")
	}
}

func TestAsyncDeliversThenClosesDrained(t *testing.T) {
	var buf syncBuffer
	a := NewAsync(NewObserver(newTestBuilder(t, testEnv()), nil, &buf), 16)
	a.Observe("man.toc", json.RawMessage(`{"page":"ls"}`))
	a.Observe("folio.read", json.RawMessage(`{"file_path":"/home/u/x"}`))
	a.Close()
	var recs []Record
	for _, line := range bytes.Split(bytes.TrimSpace([]byte(buf.String())), []byte("\n")) {
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		recs = append(recs, r)
	}
	if len(recs) != 2 || recs[0].Tool != "man.toc" || recs[1].Tool != "folio.read" {
		t.Fatalf("records = %+v", recs)
	}
	a.Observe("man.toc", nil) // after Close: must not panic or block
}

func TestAsyncSurvivesPanickingEvaluatorAndBadJSON(t *testing.T) {
	var buf syncBuffer
	a := NewAsync(NewObserver(newTestBuilder(t, testEnv()), &fakeEvaluator{panics: true}, &buf), 16)
	a.Observe("man.toc", json.RawMessage(`{not json`))
	a.Observe("man.toc", json.RawMessage(`{"page":"ls"}`))
	a.Observe("man.toc", json.RawMessage(`{"page":"ls"}`))
	a.Close()
	if n := bytes.Count([]byte(buf.String()), []byte("\n")); n < 2 {
		t.Fatalf("worker died after a bad call: %d lines logged: %s", n, buf.String())
	}
}

func TestAsyncCopiesArgsBeforeReturning(t *testing.T) {
	var buf syncBuffer
	a := NewAsync(NewObserver(newTestBuilder(t, testEnv()), nil, &buf), 16)
	raw := json.RawMessage(`{"page":"ls"}`)
	a.Observe("man.toc", raw)
	for i := range raw {
		raw[i] = 'x' // the caller reuses its buffer
	}
	a.Close()
	var r Record
	if err := json.Unmarshal(bytes.TrimSpace([]byte(buf.String())), &r); err != nil {
		t.Fatalf("log = %q: %v", buf.String(), err)
	}
	if r.Verdict != VerdictUnevaluated {
		t.Errorf("record = %+v; the args were read after the caller overwrote them", r)
	}
}

func TestOpenLogIsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "log", "moxy")
	f, err := OpenLog(dir, "sess/1")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("log mode = %o, want 600", got)
	}
	if filepath.Dir(f.Name()) != filepath.Join(dir, "shadow") {
		t.Errorf("log at %s, want under %s", f.Name(), filepath.Join(dir, "shadow"))
	}
	if filepath.Base(f.Name()) == "sess/1.jsonl" {
		t.Errorf("session id was not sanitised: %s", f.Name())
	}
	dinfo, err := os.Stat(filepath.Join(dir, "shadow"))
	if err != nil {
		t.Fatal(err)
	}
	if dinfo.Mode().Perm()&0o077 != 0 {
		t.Errorf("shadow dir mode = %o, want no group/other access", dinfo.Mode().Perm())
	}
}

func TestParseRemote(t *testing.T) {
	cases := []struct {
		url, repo, host string
		ok              bool
	}{
		{"git@forge.example:owner/name.git", "owner/name", "forge.example", true},
		{"https://forge.example/owner/name", "owner/name", "forge.example", true},
		{"https://forge.example/owner/name.git", "owner/name", "forge.example", true},
		{"ssh://git@forge.example:2222/owner/name.git", "owner/name", "forge.example", true},
		{"https://user:pw@forge.example/owner/name.git", "owner/name", "forge.example", true},
		{"/local/path/repo", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		repo, host, ok := ParseRemote(c.url)
		if repo != c.repo || host != c.host || ok != c.ok {
			t.Errorf("ParseRemote(%q) = %q, %q, %v; want %q, %q, %v", c.url, repo, host, ok, c.repo, c.host, c.ok)
		}
	}
}
