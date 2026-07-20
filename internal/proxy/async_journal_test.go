package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/asyncjob"
	"code.linenisgreat.com/moxy/internal/native"
)

// proxyFakeMadder is a minimal MadderBackend backed by an in-memory map. Only
// CatBytes is exercised by async-result's blob read; Write/OpenBlob are stubs.
type proxyFakeMadder struct{ blobs map[string][]byte }

func newProxyFakeMadder() *proxyFakeMadder { return &proxyFakeMadder{blobs: map[string][]byte{}} }

func (f *proxyFakeMadder) put(digest string, content []byte) { f.blobs[digest] = content }

func (f *proxyFakeMadder) Write(context.Context, io.Reader) (string, error) {
	return "", fmt.Errorf("proxyFakeMadder.Write not implemented")
}

func (f *proxyFakeMadder) OpenBlob(context.Context, string) (*os.File, native.BlobWriter, error) {
	return nil, nil, fmt.Errorf("proxyFakeMadder.OpenBlob not implemented")
}

func (f *proxyFakeMadder) CatBytes(_ context.Context, digest string) ([]byte, error) {
	b, ok := f.blobs[digest]
	if !ok {
		return nil, fmt.Errorf("blob %s not found", digest)
	}
	return b, nil
}

// writeJournalFile writes NDJSON journal records to a temp file the ringmaster
// stub cats for `read`.
func writeJournalFile(t *testing.T, lines ...string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "journal")
	if err := os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return f
}

// writeProxyRingmasterStub writes a ringmaster stub that echoes startID for
// `start` (empty = no job launched) and cats journalFile for `read` (empty =
// exit 1, the no-journal signal). Other verbs (done/spool-path/status) no-op.
func writeProxyRingmasterStub(t *testing.T, startID, journalFile string) string {
	t.Helper()
	shell, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("no bash on PATH for ringmaster stub: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "ringmaster")
	script := "#!" + shell + "\ncase \"$1\" in\n"
	if startID != "" {
		script += "  start) echo " + startID + " ;;\n"
	} else {
		script += "  start) : ;;\n"
	}
	if journalFile != "" {
		script += "  read) cat " + journalFile + " ;;\n"
	} else {
		script += "  read) exit 1 ;;\n"
	}
	script += "  *) : ;;\nesac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func waitLocalState(t *testing.T, m *asyncjob.Manager, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if snap, ok := m.Lookup(id); ok && snap.State == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s never reached state %q", id, want)
}

func waitLocalTerminal(t *testing.T, m *asyncjob.Manager, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if snap, ok := m.Lookup(id); ok && snap.State != asyncjob.StateRunning {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s never reached a terminal state", id)
}

func asyncJournalProxy(t *testing.T, madder *proxyFakeMadder, mgr *asyncjob.Manager) *Proxy {
	t.Helper()
	p := &Proxy{}
	p.SetMadderClient(madder)
	p.SetAsyncManager(mgr)
	return p
}

// TestHandleAsyncResultJournalFirstCrossSession is the #321 win: a job this
// moxy never launched (no local index entry) resolves entirely from the shared
// journal (terminal record + result_ref) and the moxy-async store.
func TestHandleAsyncResultJournalFirstCrossSession(t *testing.T) {
	madder := newProxyFakeMadder()
	stored := &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "hello from journal"}},
	}
	body, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	madder.put("blake2b256-xsession", body)

	journal := writeJournalFile(
		t,
		`{"job":"rg.search-xsession","type":"started","ts":"2026-06-13T00:00:00Z"}`,
		`{"job":"rg.search-xsession","type":"succeeded","ts":"2026-06-13T00:01:00Z","message":"412 matches","result_ref":"madder://blobs/blake2b256-xsession"}`,
	)
	mgr := asyncjob.New(asyncjob.Options{
		RingmasterBin: writeProxyRingmasterStub(t, "", journal),
		WriteResult:   func(context.Context, []byte) (string, error) { return "unused", nil },
		MaxRuntime:    time.Minute,
	})
	p := asyncJournalProxy(t, madder, mgr)

	res, err := p.HandleAsyncResult(context.Background(),
		json.RawMessage(`{"job_id":"rg.search-xsession"}`))
	if err != nil {
		t.Fatalf("HandleAsyncResult: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	if len(res.Content) == 0 || res.Content[0].Text != "hello from journal" {
		t.Fatalf("result = %+v, want the stored journal result", res)
	}
}

// TestHandleAsyncResultTimeoutUpgrade: a local job times out (local state =
// timeout) while the journal records the wire state `interrupted`. async-result
// upgrades interrupted -> timeout for our own job.
func TestHandleAsyncResultTimeoutUpgrade(t *testing.T) {
	journal := writeJournalFile(
		t,
		`{"job":"slow-deadbeef","type":"started","ts":"2026-06-13T00:00:00Z"}`,
		`{"job":"slow-deadbeef","type":"interrupted","ts":"2026-06-13T00:00:01Z","message":"timed out after 20ms"}`,
	)
	mgr := asyncjob.New(asyncjob.Options{
		RingmasterBin: writeProxyRingmasterStub(t, "slow-deadbeef", journal),
		WriteResult:   func(context.Context, []byte) (string, error) { return "unused", nil },
		MaxRuntime:    20 * time.Millisecond,
	})
	p := asyncJournalProxy(t, newProxyFakeMadder(), mgr)

	id, err := mgr.Dispatch(context.Background(), "slow", json.RawMessage(`{}`), 0,
		func(ctx context.Context, _ string, _ json.RawMessage) (*protocol.ToolCallResultV1, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	waitLocalState(t, mgr, id, asyncjob.StateTimeout)

	res, err := p.HandleAsyncResult(context.Background(),
		json.RawMessage(`{"job_id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("HandleAsyncResult: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("parsing result %q: %v", res.Content[0].Text, err)
	}
	if out["status"] != asyncjob.StateTimeout {
		t.Errorf("status = %v, want %q (upgraded from journal interrupted)", out["status"], asyncjob.StateTimeout)
	}
}

// TestHandleAsyncResultBlobReapedFallsBackToLocal: the journal terminal
// references a result blob that is NOT in the store (reaped per clown#126).
// async-result falls back to the locally-cached result.
func TestHandleAsyncResultBlobReapedFallsBackToLocal(t *testing.T) {
	journal := writeJournalFile(
		t,
		`{"job":"rg.search-reaped","type":"started","ts":"2026-06-13T00:00:00Z"}`,
		`{"job":"rg.search-reaped","type":"succeeded","ts":"2026-06-13T00:01:00Z","message":"ok","result_ref":"madder://blobs/blake2b256-gone"}`,
	)
	mgr := asyncjob.New(asyncjob.Options{
		RingmasterBin: writeProxyRingmasterStub(t, "rg.search-reaped", journal),
		WriteResult:   func(context.Context, []byte) (string, error) { return "blake2b256-local", nil },
		MaxRuntime:    time.Minute,
	})
	// Empty store: blake2b256-gone is absent, so the journal blob fetch fails.
	p := asyncJournalProxy(t, newProxyFakeMadder(), mgr)

	want := &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "cached locally"}},
	}
	id, err := mgr.Dispatch(context.Background(), "rg.search", json.RawMessage(`{}`), 0,
		func(context.Context, string, json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return want, nil
		})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	waitLocalTerminal(t, mgr, id)

	res, err := p.HandleAsyncResult(context.Background(),
		json.RawMessage(`{"job_id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("HandleAsyncResult: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	if len(res.Content) == 0 || res.Content[0].Text != "cached locally" {
		t.Fatalf("result = %+v, want the locally-cached result (journal blob reaped)", res)
	}
}

// writeRunningRingmasterStub writes a ringmaster stub whose `read` cats
// journalFile (a running, no-terminal journal) and whose `status` runs
// statusCmd (a bash snippet — e.g. a printf of live JSON, or `exit 1`).
func writeRunningRingmasterStub(t *testing.T, journalFile, statusCmd string) string {
	t.Helper()
	shell, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("no bash on PATH for ringmaster stub: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "ringmaster")
	script := "#!" + shell + "\ncase \"$1\" in\n" +
		"  read) cat " + journalFile + " ;;\n" +
		"  status) " + statusCmd + " ;;\n" +
		"  *) : ;;\nesac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// The running-job branch of HandleAsyncResult surfaces clown's live status
// probe (RFC-0010 §3 / FDR-0005): a job whose journal carries a started record
// but no terminal one reads back as `running`, and mergeLiveStatus merges
// elapsed_sec/last_activity/spool_bytes/progress/tail from `ringmaster status`
// verbatim onto the response. #352 — the proxy-level merge wiring had no direct
// assertion; asyncjob.JobStatus's parse was unit-tested, but not that
// async-result actually carries the merged fields on a running job.
func TestHandleAsyncResultMergesRunningStatus(t *testing.T) {
	journal := writeJournalFile(t,
		`{"job":"rg.search-live","type":"started","ts":"2026-06-13T00:00:00Z"}`)
	status := `{"state":"running","elapsed_sec":312,"last_activity":"2026-06-08T00:17:40Z",` +
		`"spool_bytes":48211,"progress":"step 3/5","tail":"moxy-bats-grit> ok 24"}`
	mgr := asyncjob.New(asyncjob.Options{
		RingmasterBin: writeRunningRingmasterStub(t, journal, "printf '%s' '"+status+"'"),
		WriteResult:   func(context.Context, []byte) (string, error) { return "unused", nil },
		MaxRuntime:    time.Minute,
	})
	p := asyncJournalProxy(t, newProxyFakeMadder(), mgr)

	res, err := p.HandleAsyncResult(context.Background(),
		json.RawMessage(`{"job_id":"rg.search-live"}`))
	if err != nil {
		t.Fatalf("HandleAsyncResult: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("parsing result %q: %v", res.Content[0].Text, err)
	}
	if out["status"] != asyncjob.StateRunning {
		t.Errorf("status = %v, want running", out["status"])
	}
	// The FDR-0005 merged spool fields must ride back on the running response.
	// JSON numbers decode as float64.
	if out["elapsed_sec"] != float64(312) {
		t.Errorf("elapsed_sec = %v, want 312", out["elapsed_sec"])
	}
	if out["spool_bytes"] != float64(48211) {
		t.Errorf("spool_bytes = %v, want 48211", out["spool_bytes"])
	}
	if out["last_activity"] != "2026-06-08T00:17:40Z" {
		t.Errorf("last_activity = %v, want the probe value", out["last_activity"])
	}
	if out["progress"] != "step 3/5" {
		t.Errorf("progress = %v, want the probe value", out["progress"])
	}
	if out["tail"] != "moxy-bats-grit> ok 24" {
		t.Errorf("tail = %v, want the probe value", out["tail"])
	}
}

// When the live-status probe fails (channel disabled, ringmaster absent, or an
// installed ringmaster without the probe), a running-job response keeps its
// base shape — the probe is a façade, never a hard dependency (RFC-0010 §3).
func TestHandleAsyncResultRunningKeepsBaseShapeWhenProbeFails(t *testing.T) {
	journal := writeJournalFile(t,
		`{"job":"rg.search-noprobe","type":"started","ts":"2026-06-13T00:00:00Z"}`)
	mgr := asyncjob.New(asyncjob.Options{
		RingmasterBin: writeRunningRingmasterStub(t, journal, "exit 1"),
		WriteResult:   func(context.Context, []byte) (string, error) { return "unused", nil },
		MaxRuntime:    time.Minute,
	})
	p := asyncJournalProxy(t, newProxyFakeMadder(), mgr)

	res, err := p.HandleAsyncResult(context.Background(),
		json.RawMessage(`{"job_id":"rg.search-noprobe"}`))
	if err != nil {
		t.Fatalf("HandleAsyncResult: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("parsing result %q: %v", res.Content[0].Text, err)
	}
	if out["status"] != asyncjob.StateRunning {
		t.Errorf("status = %v, want running", out["status"])
	}
	for _, f := range []string{"elapsed_sec", "last_activity", "spool_bytes", "progress", "tail"} {
		if _, ok := out[f]; ok {
			t.Errorf("field %q present, want base shape when the probe fails", f)
		}
	}
}
