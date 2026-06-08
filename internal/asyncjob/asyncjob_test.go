package asyncjob

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// writeClownStub writes an argv-recording clown stub. `job start` prints
// startOutput (empty string = the CLOWN_DISABLE_JOB_WAKEUP=1 no-op
// signature: exit 0, no output).
func writeClownStub(t *testing.T, startOutput string) (bin, record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	bin = filepath.Join(dir, "clown")
	script := "#!/usr/bin/env bash\n" +
		"printf '%s\\n' \"$*\" >> " + record + "\n" +
		"if [ \"$1\" = job ] && [ \"$2\" = start ]; then\n"
	if startOutput != "" {
		script += "  echo " + startOutput + "\n"
	}
	script += "fi\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, record
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

// waitDoneLine polls the stub record for a `job done` line. The index goes
// terminal BEFORE the done emit completes (a hung clown must not make jobs
// look stuck), so tests asserting on the emit must poll.
func waitDoneLine(t *testing.T, record string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		lines := recordLines(t, record)
		if n := len(lines); n > 0 && strings.Contains(lines[n-1], "job done") {
			return lines[n-1]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("clown stub never recorded a job done line")
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

func newTestManager(clownBin string) *Manager {
	return New(Options{
		ClownBin: clownBin,
		// Fake result writer: digest is derived from length so tests can
		// assert it flowed through without real madder.
		WriteResult: func(_ context.Context, content []byte) (string, error) {
			return "fakedigest-" + strings.Repeat("x", 4), nil
		},
		MaxRuntime:  time.Minute,
		Concurrency: 4,
	})
}

func TestProducerStartAdoptsClownID(t *testing.T) {
	// clown's label sanitizer KEEPS dots (job-id charset [A-Za-z0-9._-]).
	bin, record := writeClownStub(t, "rg.search-3f2a8b1c")
	m := newTestManager(bin)

	id, err := m.Dispatch(context.Background(), "rg.search", nil, 0,
		func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return okResult("412 matches"), nil
		})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if id != "rg.search-3f2a8b1c" {
		t.Errorf("id = %q, want clown-issued id", id)
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
	if len(lines) != 2 {
		t.Fatalf("clown invocations = %v, want start+done", lines)
	}
	if !strings.Contains(lines[0], "job start --source moxy --label rg.search") {
		t.Errorf("start line = %q", lines[0])
	}
	for _, want := range []string{
		"job done rg.search-3f2a8b1c",
		"--state succeeded",
		"412 matches",
		snap.Digest,
		"--result-ref moxy async-result rg.search-3f2a8b1c",
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

// CLOWN_DISABLE_JOB_WAKEUP=1 contract: `job start` exits 0 printing NOTHING.
// moxy must mint a local id of the same shape and async must keep working.
func TestEmptyStdoutMintsLocalID(t *testing.T) {
	bin, _ := writeClownStub(t, "")
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

func TestMissingClownBinStillDispatches(t *testing.T) {
	m := newTestManager("/nonexistent/clown")
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

func TestDispatchErrorIsFailed(t *testing.T) {
	bin, record := writeClownStub(t, "boom-11112222")
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
	bin, _ := writeClownStub(t, "boom-33334444")
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
	bin, record := writeClownStub(t, "slow-55556666")
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
	bin, record := writeClownStub(t, "slow-77778888")
	m := New(Options{
		ClownBin: bin,
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
	bin, record := writeClownStub(t, "slow-aaaa1111")
	m := New(Options{
		ClownBin: bin,
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
	bin, record := writeClownStub(t, "slow-9999aaaa")
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
	bin, record := writeClownStub(t, "res-dddd0000")
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
	bin, _ := writeClownStub(t, "res-bbbbcccc")
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
