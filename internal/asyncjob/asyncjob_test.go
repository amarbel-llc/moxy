package asyncjob

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// writeRingmasterStub writes an argv-recording ringmaster stub. `start` prints
// startOutput (empty string = the CLOWN_DISABLE_JOB_WAKEUP=1 no-op
// signature: exit 0, no output).
func writeRingmasterStub(t *testing.T, startOutput string) (bin, record string) {
	t.Helper()
	// Resolve a real interpreter rather than hardcoding `/usr/bin/env bash`:
	// the hermetic nix-check sandbox has bash on PATH but no /usr/bin/env, so
	// an env-shebang stub silently fails to exec and `startJob` mints a local
	// id instead of running the stub. A direct shebang to the resolved bash
	// works in both the sandbox and the devshell.
	shell, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("no bash on PATH for ringmaster stub: %v", err)
	}
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	bin = filepath.Join(dir, "ringmaster")
	script := "#!" + shell + "\n" +
		"printf '%s\\n' \"$*\" >> " + record + "\n" +
		"if [ \"$1\" = start ]; then\n"
	if startOutput != "" {
		script += "  echo " + startOutput + "\n"
	}
	script += "fi\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, record
}

// writeRingmasterScript writes an executable ringmaster stub whose body is
// `body` (a bash snippet receiving the `ringmaster` argv as $@). Used by the
// spool-path / status tests where the stub needs custom per-verb output.
func writeRingmasterScript(t *testing.T, body string) string {
	t.Helper()
	shell, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("no bash on PATH for ringmaster stub: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "ringmaster")
	if err := os.WriteFile(bin, []byte("#!"+shell+"\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestResolveSpoolPath(t *testing.T) {
	bin := writeRingmasterScript(t, `[ "$1" = spool-path ] && echo "/spool/$2.out"`)
	m := newTestManager(bin)
	if got := m.resolveSpoolPath(context.Background(), "rg.search-1a2b"); got != "/spool/rg.search-1a2b.out" {
		t.Errorf("resolveSpoolPath = %q, want /spool/rg.search-1a2b.out", got)
	}
}

func TestResolveSpoolPathEmptyOnDisabledOrAbsent(t *testing.T) {
	// Disabled channel: spool-path exits 0 with no output.
	empty := writeRingmasterScript(t, `exit 0`)
	if got := newTestManager(empty).resolveSpoolPath(context.Background(), "x"); got != "" {
		t.Errorf("disabled: got %q, want empty", got)
	}
	// Absent ringmaster / old ringmaster without the verb: exec fails.
	if got := newTestManager("/nonexistent/ringmaster").resolveSpoolPath(context.Background(), "x"); got != "" {
		t.Errorf("absent: got %q, want empty", got)
	}
}

func TestJobStatusParsesJSON(t *testing.T) {
	bin := writeRingmasterScript(t, `[ "$1" = status ] && echo '{"state":"running","elapsed_sec":42,"last_activity":"2026-06-08T00:00:00Z","spool_bytes":17,"tail":"hello"}'`)
	m := newTestManager(bin)
	st, err := m.JobStatus(context.Background(), "x.y-1")
	if err != nil {
		t.Fatalf("JobStatus: %v", err)
	}
	if st["tail"] != "hello" {
		t.Errorf("tail = %v, want hello", st["tail"])
	}
	if st["spool_bytes"].(float64) != 17 { // JSON numbers decode as float64
		t.Errorf("spool_bytes = %v, want 17", st["spool_bytes"])
	}
}

func TestJobStatusErrorWhenRingmasterFails(t *testing.T) {
	// A locally-minted id has no journal → ringmaster status exits 1.
	bin := writeRingmasterScript(t, `exit 1`)
	if _, err := newTestManager(bin).JobStatus(context.Background(), "x"); err == nil {
		t.Error("want error when ringmaster status exits 1")
	}
	if _, err := newTestManager("/nonexistent/ringmaster").JobStatus(context.Background(), "x"); err == nil {
		t.Error("want error when ringmaster is absent")
	}
}

func TestReadJournalParsesTerminal(t *testing.T) {
	body := `[ "$1" = read ] && printf '%s\n' ` +
		`'{"job":"x.y-1","type":"started","ts":"2026-06-13T00:00:00Z"}' ` +
		`'{"job":"x.y-1","type":"succeeded","ts":"2026-06-13T00:01:00Z","message":"done","result_ref":"madder://blobs/abc123"}'`
	m := newTestManager(writeRingmasterScript(t, body))
	view, err := m.ReadJournal(context.Background(), "x.y-1")
	if err != nil {
		t.Fatalf("ReadJournal: %v", err)
	}
	if !view.Found {
		t.Fatal("Found = false, want true")
	}
	if view.State != StateSucceeded {
		t.Errorf("State = %q, want %q", view.State, StateSucceeded)
	}
	if view.ResultRef != "madder://blobs/abc123" {
		t.Errorf("ResultRef = %q, want madder://blobs/abc123", view.ResultRef)
	}
	if view.Message != "done" {
		t.Errorf("Message = %q, want done", view.Message)
	}
	if view.Started.IsZero() || view.Ended.IsZero() {
		t.Errorf("timestamps not parsed: started=%v ended=%v", view.Started, view.Ended)
	}
}

// A stream with no terminal record means the job is still running — it must
// NOT be inferred dead (RFC-0010 §3: status is journal-derived and never
// detects a producer that died without a terminal).
func TestReadJournalRunningNoTerminal(t *testing.T) {
	body := `[ "$1" = read ] && printf '%s\n' ` +
		`'{"job":"x.y-1","type":"started","ts":"2026-06-13T00:00:00Z"}' ` +
		`'{"job":"x.y-1","type":"progress","ts":"2026-06-13T00:00:30Z","message":"working"}'`
	m := newTestManager(writeRingmasterScript(t, body))
	view, err := m.ReadJournal(context.Background(), "x.y-1")
	if err != nil {
		t.Fatalf("ReadJournal: %v", err)
	}
	if !view.Found {
		t.Fatal("Found = false, want true (records present)")
	}
	if view.State != "" {
		t.Errorf("State = %q, want empty (no terminal record)", view.State)
	}
}

func TestReadJournalErrorWhenRingmasterFails(t *testing.T) {
	// Disabled channel / locally-minted id with no journal → exit 1.
	if _, err := newTestManager(writeRingmasterScript(t, `exit 1`)).ReadJournal(context.Background(), "x"); err == nil {
		t.Error("want error when ringmaster read exits 1")
	}
	// Absent ringmaster.
	if _, err := newTestManager("/nonexistent/ringmaster").ReadJournal(context.Background(), "x"); err == nil {
		t.Error("want error when ringmaster is absent")
	}
}

func recordLines(t *testing.T, record string) []string {
	t.Helper()
	data, err := os.ReadFile(record)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// waitDoneLine polls the stub record for a `done` line. The index goes
// terminal BEFORE the done emit completes (a hung ringmaster must not make jobs
// look stuck), so tests asserting on the emit must poll.
func waitDoneLine(t *testing.T, record string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		lines := recordLines(t, record)
		if n := len(lines); n > 0 && strings.Contains(lines[n-1], "done") {
			return lines[n-1]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("ringmaster stub never recorded a done line")
	return ""
}

func okResult(text string) *protocol.ToolCallResultV1 {
	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{{Type: "text", Text: text}},
	}
}

// waitTerminal polls until the job leaves StateRunning.
func waitTerminal(t *testing.T, m *Manager, id string) Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap, ok := m.Lookup(id)
		if !ok {
			t.Fatalf("job %s vanished", id)
		}
		if snap.State != StateRunning {
			return snap
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s never reached a terminal state", id)
	return Snapshot{}
}

func newTestManager(ringmasterBin string) *Manager {
	return New(Options{
		RingmasterBin: ringmasterBin,
		// Fake result writer: digest is derived from length so tests can
		// assert it flowed through without real madder.
		WriteResult: func(_ context.Context, content []byte) (string, error) {
			return "fakedigest-" + strings.Repeat("x", 4), nil
		},
		MaxRuntime:  time.Minute,
		Concurrency: 4,
	})
}

func TestProducerStartAdoptsRingmasterID(t *testing.T) {
	// ringmaster's label sanitizer KEEPS dots (job-id charset [A-Za-z0-9._-]).
	bin, record := writeRingmasterStub(t, "rg.search-3f2a8b1c")
	m := newTestManager(bin)

	id, err := m.Dispatch(context.Background(), "rg.search", nil, 0,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return okResult("412 matches"), nil
		})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if id != "rg.search-3f2a8b1c" {
		t.Errorf("id = %q, want ringmaster-issued id", id)
	}

	snap := waitTerminal(t, m, id)
	if snap.State != StateSucceeded {
		t.Errorf("state = %q, want succeeded", snap.State)
	}
	if snap.Digest == "" {
		t.Error("digest not recorded")
	}

	done := waitDoneLine(t, record)
	lines := recordLines(t, record)
	// start, then spool-path (FDR-0005 resolves the spool before dispatch),
	// then done.
	if len(lines) != 3 {
		t.Fatalf("ringmaster invocations = %v, want start+spool-path+done", lines)
	}
	if !strings.Contains(lines[0], "start --source moxy --label rg.search") {
		t.Errorf("start line = %q", lines[0])
	}
	if !strings.Contains(lines[1], "spool-path rg.search-3f2a8b1c") {
		t.Errorf("second invocation = %q, want spool-path", lines[1])
	}
	for _, want := range []string{
		"done rg.search-3f2a8b1c",
		"--state succeeded",
		"412 matches",
		snap.Digest,
		"--result-ref madder://blobs/" + snap.Digest,
	} {
		if !strings.Contains(done, want) {
			t.Errorf("done line %q missing %q", done, want)
		}
	}
	// The state must not be repeated in the --message text (the wake line
	// renders it from the record's state field).
	if strings.Contains(done, "rg.search succeeded:") {
		t.Errorf("done line %q repeats the state in --message", done)
	}
}

// CLOWN_DISABLE_JOB_WAKEUP=1 contract: `ringmaster start` exits 0 printing
// NOTHING. moxy must mint a local id of the same shape and async keeps working.
func TestEmptyStdoutMintsLocalID(t *testing.T) {
	bin, _ := writeRingmasterStub(t, "")
	m := newTestManager(bin)

	id, err := m.Dispatch(context.Background(), "rg.search", nil, 0,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return okResult("ok"), nil
		})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !regexp.MustCompile(`^rg\.search-[0-9a-f]{8}$`).MatchString(id) {
		t.Errorf("minted id = %q, want <label>-<8hex>", id)
	}
	snap := waitTerminal(t, m, id)
	if snap.State != StateSucceeded {
		t.Errorf("state = %q, want succeeded", snap.State)
	}
}

func TestMissingRingmasterBinStillDispatches(t *testing.T) {
	m := newTestManager("/nonexistent/ringmaster")
	id, err := m.Dispatch(context.Background(), "grit.status", nil, 0,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return okResult("clean"), nil
		})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	snap := waitTerminal(t, m, id)
	if snap.State != StateSucceeded {
		t.Errorf("state = %q, want succeeded", snap.State)
	}
}

// The nix-built moxy burns in a pinned ringmaster store path via ldflag
// (defaultRingmasterBin, clown RFC-0015). It is the default when neither
// Options.RingmasterBin nor $RINGMASTER_BIN is set; $RINGMASTER_BIN still
// overrides it (the test/pinning seam), and an explicit Options value wins
// over both.
func TestRingmasterBinResolutionPrecedence(t *testing.T) {
	old := defaultRingmasterBin
	defaultRingmasterBin = "/nix/store/pinned/bin/ringmaster"
	t.Cleanup(func() { defaultRingmasterBin = old })

	writer := func(context.Context, []byte) (string, error) { return "", nil }

	// Burned-in default applies when Options + env are both empty.
	t.Setenv("RINGMASTER_BIN", "")
	if got := New(Options{WriteResult: writer}).ringmasterBin; got != defaultRingmasterBin {
		t.Errorf("ringmasterBin = %q, want the burned-in default %q", got, defaultRingmasterBin)
	}

	// $RINGMASTER_BIN overrides the burned-in default.
	t.Setenv("RINGMASTER_BIN", "/override/ringmaster")
	if got := New(Options{WriteResult: writer}).ringmasterBin; got != "/override/ringmaster" {
		t.Errorf("ringmasterBin = %q, want the env override", got)
	}

	// An explicit Options.RingmasterBin wins over everything.
	if got := New(Options{RingmasterBin: "/explicit/ringmaster", WriteResult: writer}).ringmasterBin; got != "/explicit/ringmaster" {
		t.Errorf("ringmasterBin = %q, want the explicit option", got)
	}
}

func TestDispatchErrorIsFailed(t *testing.T) {
	bin, record := writeRingmasterStub(t, "boom-11112222")
	m := newTestManager(bin)
	id, _ := m.Dispatch(context.Background(), "x.boom", nil, 0,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return nil, errors.New("dispatch exploded")
		})
	snap := waitTerminal(t, m, id)
	if snap.State != StateFailed {
		t.Errorf("state = %q, want failed", snap.State)
	}
	if done := waitDoneLine(t, record); !strings.Contains(done, "--state failed") {
		t.Errorf("done line = %q, want failed", done)
	}
}

func TestIsErrorResultIsFailed(t *testing.T) {
	bin, _ := writeRingmasterStub(t, "boom-33334444")
	m := newTestManager(bin)
	id, _ := m.Dispatch(context.Background(), "x.boom", nil, 0,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return &protocol.ToolCallResultV1{
				IsError: true,
				Content: []protocol.ContentBlockV1{{Type: "text", Text: "tool-level error"}},
			}, nil
		})
	snap := waitTerminal(t, m, id)
	if snap.State != StateFailed {
		t.Errorf("state = %q, want failed", snap.State)
	}
}

func TestCancelProducesCancelled(t *testing.T) {
	bin, record := writeRingmasterStub(t, "slow-55556666")
	m := newTestManager(bin)
	started := make(chan struct{})
	id, _ := m.Dispatch(context.Background(), "x.slow", nil, 0,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	<-started
	if err := m.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	snap := waitTerminal(t, m, id)
	if snap.State != StateCancelled {
		t.Errorf("state = %q, want cancelled", snap.State)
	}
	if done := waitDoneLine(t, record); !strings.Contains(done, "--state cancelled") {
		t.Errorf("done line = %q, want cancelled", done)
	}
	// Cancelling a terminal job is a no-op.
	if err := m.Cancel(id); err != nil {
		t.Errorf("Cancel on terminal job: %v", err)
	}
}

// The default max-runtime deadline produces the moxy status `timeout`, which
// async-result reports as-is, but emits clown wire state `interrupted` (#345).
func TestMaxRuntimeProducesTimeout(t *testing.T) {
	bin, record := writeRingmasterStub(t, "slow-77778888")
	m := New(Options{
		RingmasterBin: bin,
		WriteResult: func(_ context.Context, _ []byte) (string, error) {
			return "d", nil
		},
		MaxRuntime:  20 * time.Millisecond,
		Concurrency: 4,
	})
	id, _ := m.Dispatch(context.Background(), "x.slow", nil, 0,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
	snap := waitTerminal(t, m, id)
	if snap.State != StateTimeout {
		t.Errorf("state = %q, want timeout", snap.State)
	}
	if !strings.Contains(snap.Summary, "timed out after") {
		t.Errorf("summary = %q, want a 'timed out after' phrasing", snap.Summary)
	}
	// clown accepts only the four wire states; timeout maps to interrupted.
	if done := waitDoneLine(t, record); !strings.Contains(done, "--state interrupted") {
		t.Errorf("done line = %q, want wire state interrupted", done)
	}
}

// A per-call timeout overrides a generous manager default and terminalizes the
// same way as the default deadline (#345).
func TestPerCallTimeoutOverridesDefault(t *testing.T) {
	bin, record := writeRingmasterStub(t, "slow-aaaa1111")
	m := New(Options{
		RingmasterBin: bin,
		WriteResult: func(_ context.Context, _ []byte) (string, error) {
			return "d", nil
		},
		MaxRuntime:  time.Hour, // would never fire within the test
		Concurrency: 4,
	})
	id, _ := m.Dispatch(context.Background(), "x.slow", nil, 20*time.Millisecond,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
	snap := waitTerminal(t, m, id)
	if snap.State != StateTimeout {
		t.Errorf("state = %q, want timeout (per-call timeout should fire before the 1h default)", snap.State)
	}
	if !strings.Contains(snap.Summary, "20ms") {
		t.Errorf("summary = %q, want the per-call duration", snap.Summary)
	}
	if done := waitDoneLine(t, record); !strings.Contains(done, "--state interrupted") {
		t.Errorf("done line = %q, want wire state interrupted", done)
	}
}

func TestSweepInterruptsInFlight(t *testing.T) {
	bin, record := writeRingmasterStub(t, "slow-9999aaaa")
	m := newTestManager(bin)
	started := make(chan struct{})
	id, _ := m.Dispatch(context.Background(), "x.slow", nil, 0,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	<-started
	m.Sweep()
	snap, _ := m.Lookup(id)
	if snap.State != StateInterrupted {
		t.Errorf("state = %q, want interrupted after sweep", snap.State)
	}
	lines := recordLines(t, record)
	if !strings.Contains(lines[len(lines)-1], "--state interrupted") {
		t.Errorf("done line = %q, want interrupted", lines[len(lines)-1])
	}
}

// Native moxin results with a content-type arrive as embedded-resource
// blocks (text in Resource.Text, not the top-level Text) — the wake-line
// summary must come from there too. Round-trip through JSON to mirror the
// proxy's decodeToolCallResult path exactly.
func TestSummaryFromEmbeddedResourceBlock(t *testing.T) {
	bin, record := writeRingmasterStub(t, "res-dddd0000")
	m := newTestManager(bin)

	raw := []byte(`{"content":[{"type":"resource","resource":{"uri":"moxy.native://x","text":"hello async","mimeType":"text/plain"}}]}`)
	var result protocol.ToolCallResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	id, _ := m.Dispatch(context.Background(), "x.res", nil, 0,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return &result, nil
		})
	snap := waitTerminal(t, m, id)
	if snap.Summary != "hello async" {
		t.Errorf("summary = %q, want %q", snap.Summary, "hello async")
	}
	done := waitDoneLine(t, record)
	if !strings.Contains(done, "x.res: hello async") {
		t.Errorf("done line = %q, want summary in message", done)
	}
}

func TestLookupReturnsStoredResult(t *testing.T) {
	bin, _ := writeRingmasterStub(t, "res-bbbbcccc")
	m := newTestManager(bin)
	id, _ := m.Dispatch(context.Background(), "x.res", nil, 0,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return okResult("the full result body"), nil
		})
	snap := waitTerminal(t, m, id)
	if snap.Result == nil || len(snap.Result.Content) == 0 ||
		snap.Result.Content[0].Text != "the full result body" {
		t.Errorf("stored result = %+v", snap.Result)
	}

	if _, ok := m.Lookup("no-such-id"); ok {
		t.Error("Lookup of unknown id reported ok")
	}
}
