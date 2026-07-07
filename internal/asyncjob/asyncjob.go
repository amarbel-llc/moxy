// Package asyncjob backgrounds moxy tool dispatches: each job runs on a
// detached context, writes its full result through a pluggable writer
// (production: a user-level madder store), and reports terminal states over
// clown's job-wakeup channel by shelling out to ${RINGMASTER_BIN:-ringmaster}.
// clown RFC-0015 promoted the producer job verbs off `clown job <verb>` onto
// the standalone `ringmaster` binary (behavior-preserving). See
// docs/features/0004-async-tool-dispatch.md for the design and the pinned
// producer contract (RFC-0009).
package asyncjob

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"github.com/amarbel-llc/moxy/internal/lifecyclelog"
	"github.com/amarbel-llc/moxy/internal/spoolctx"
)

// Job states. Running is the only non-terminal state. Succeeded/Failed/
// Cancelled/Interrupted map one-to-one onto clown's job-wakeup wire states.
// Timeout is a moxy-only distinction (a deadline-exceeded job, #345): it is
// reported as-is to agents via async-result but emitted on clown's wire as
// `interrupted` (clown accepts only the four wire states; a deadline IS an
// interruption). See wireState.
const (
	StateRunning     = "running"
	StateSucceeded   = "succeeded"
	StateFailed      = "failed"
	StateCancelled   = "cancelled"
	StateInterrupted = "interrupted"
	StateTimeout     = "timeout"
)

// wireState maps a moxy job state onto a clown job-wakeup wire state. Only
// Timeout needs translation; every other state is already a valid wire state.
func wireState(state string) string {
	if state == StateTimeout {
		return StateInterrupted
	}
	return state
}

// DispatchFunc runs one tool call. Production passes Proxy.CallToolV1
// (wrapped to carry the tool name); tests substitute fakes.
type DispatchFunc func(ctx context.Context, tool string, args json.RawMessage) (*protocol.ToolCallResultV1, error)

// WriteResultFunc persists a marshaled ToolCallResultV1 and returns a
// digest/reference. Production writes to the user-level madder store.
type WriteResultFunc func(ctx context.Context, content []byte) (string, error)

// defaultRingmasterBin is the ringmaster binary path burned in at nix build
// time via -ldflags "-X <pkg>.defaultRingmasterBin=<store>/bin/ringmaster", so
// a packaged moxy runs a hermetic, version-pinned ringmaster (clown RFC-0015)
// without depending on ambient PATH. It is empty under a plain `go build`
// (devshell, tests), where resolution falls back to bare "ringmaster".
var defaultRingmasterBin string

// Options configures a Manager.
type Options struct {
	// RingmasterBin is the ringmaster binary to shell out to. Empty falls
	// back to $RINGMASTER_BIN (the test/override seam), then the build-time
	// pinned default (defaultRingmasterBin), then bare "ringmaster" (PATH
	// lookup at exec time — ringmaster ships on PATH wherever clown is
	// installed, RFC-0015).
	RingmasterBin string
	// WriteResult persists terminal results. Required.
	WriteResult WriteResultFunc
	// MaxRuntime bounds each job so every job reaches a terminal state.
	// Zero defaults to 30 minutes.
	MaxRuntime time.Duration
	// Concurrency caps simultaneously-running jobs. Zero defaults to 16.
	Concurrency int
}

// Snapshot is an immutable view of a job for Lookup/handlers.
type Snapshot struct {
	ID       string
	Tool     string
	State    string
	Digest   string
	Summary  string
	Started  time.Time
	Finished time.Time
	// Result is the full tool result, present once terminal (nil for
	// cancelled/interrupted jobs that produced none).
	Result *protocol.ToolCallResultV1
}

type job struct {
	Snapshot
	cancel        context.CancelFunc
	userCancelled bool
	swept         bool
	// runtime is the deadline this job ran under (per-call timeout or the
	// manager default), used to phrase the timeout summary.
	runtime time.Duration
}

// Manager owns the job index and lifecycle.
type Manager struct {
	mu   sync.Mutex
	jobs map[string]*job
	wg   sync.WaitGroup

	ringmasterBin string
	writeResult   WriteResultFunc
	maxRuntime    time.Duration
	sem           chan struct{}
}

// New builds a Manager from Options.
func New(opts Options) *Manager {
	bin := opts.RingmasterBin
	if bin == "" {
		bin = os.Getenv("RINGMASTER_BIN")
	}
	if bin == "" {
		bin = defaultRingmasterBin
	}
	if bin == "" {
		bin = "ringmaster"
	}
	maxRuntime := opts.MaxRuntime
	if maxRuntime == 0 {
		maxRuntime = 30 * time.Minute
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 16
	}
	return &Manager{
		jobs:          make(map[string]*job),
		ringmasterBin: bin,
		writeResult:   opts.WriteResult,
		maxRuntime:    maxRuntime,
		sem:           make(chan struct{}, concurrency),
	}
}

// Dispatch opens a ringmaster job (or mints a local id when the channel is
// disabled/absent), starts the call on a detached context, and returns the
// job id immediately. The permission decision is the CALLER's job — only
// allow-resolved calls may reach Dispatch. timeout overrides the manager's
// default max runtime when > 0; a deadline-exceeded job terminalizes as
// StateTimeout (#345).
func (m *Manager) Dispatch(ctx context.Context, tool string, args json.RawMessage, timeout time.Duration, dispatch DispatchFunc) (string, error) {
	id := m.startJob(ctx, tool)

	runtime := m.maxRuntime
	if timeout > 0 {
		runtime = timeout
	}
	jobCtx, cancel := context.WithTimeout(context.Background(), runtime)
	// Resolve the clown output spool (RFC-0010) and thread it down to the
	// native exec layer, which tees the child's output into it for live
	// `async-result`/`ringmaster status` probing. Empty when clown is
	// disabled/absent — the tee is then skipped (best-effort, FDR-0005).
	jobCtx = spoolctx.WithPath(jobCtx, m.resolveSpoolPath(jobCtx, id))
	j := &job{
		Snapshot: Snapshot{
			ID:      id,
			Tool:    tool,
			State:   StateRunning,
			Started: time.Now(),
		},
		cancel:  cancel,
		runtime: runtime,
	}

	m.mu.Lock()
	m.jobs[id] = j
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel()

		m.sem <- struct{}{}
		defer func() { <-m.sem }()

		result, err := dispatch(jobCtx, tool, args)
		m.finish(id, jobCtx, result, err)
	}()

	return id, nil
}

// Lookup returns a snapshot of the job, if known.
func (m *Manager) Lookup(id string) (Snapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return Snapshot{}, false
	}
	return j.Snapshot, true
}

// IDs returns all known job ids (for unknown-id error messages).
func (m *Manager) IDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.jobs))
	for id := range m.jobs {
		ids = append(ids, id)
	}
	return ids
}

// Cancel context-cancels a running job; terminal jobs are a no-op. Unknown
// ids return an error.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown async job %q", id)
	}
	if j.State != StateRunning {
		m.mu.Unlock()
		return nil
	}
	j.userCancelled = true
	cancel := j.cancel
	m.mu.Unlock()
	cancel()
	return nil
}

// Sweep cancels every running job with interrupted semantics and waits for
// their terminal `done` emits. Call on graceful shutdown so no job is left
// open in the journal (a hard crash is the accepted producer-death gap).
func (m *Manager) Sweep() {
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.State == StateRunning {
			j.swept = true
			j.cancel()
		}
	}
	m.mu.Unlock()
	m.wg.Wait()
}

// finish classifies the outcome, persists the result, updates the index,
// and emits the terminal done. Emit/persist failures are logged and never
// affect the stored job state.
func (m *Manager) finish(id string, jobCtx context.Context, result *protocol.ToolCallResultV1, err error) {
	m.mu.Lock()
	j := m.jobs[id]
	userCancelled, swept, runtime := j.userCancelled, j.swept, j.runtime
	m.mu.Unlock()

	state := classify(jobCtx, result, err, userCancelled, swept)

	var digest, summary string
	if result != nil {
		summary = firstLine(result)
		if data, merr := json.Marshal(result); merr == nil {
			d, werr := m.writeResult(context.Background(), data)
			if werr != nil {
				lifecyclelog.Log("asyncjob %s: writing result: %v", id, werr)
			} else {
				digest = d
			}
		} else {
			lifecyclelog.Log("asyncjob %s: marshaling result: %v", id, merr)
		}
	} else if err != nil {
		summary = firstLineOf(err.Error())
	}
	// A timed-out job's only error is "context deadline exceeded"; phrase it
	// for the agent instead (#345).
	if state == StateTimeout {
		summary = "timed out after " + runtime.String()
	}

	m.mu.Lock()
	j.State = state
	j.Digest = digest
	j.Summary = summary
	j.Finished = time.Now()
	j.Result = result
	m.mu.Unlock()

	m.emitDone(j.Tool, id, state, summary, digest)
}

// classify maps a dispatch outcome onto a terminal state. Order matters: an
// explicit user cancel wins over the generic ctx error; a graceful-shutdown
// sweep cancels via CancelFunc (context.Canceled, caught by the swept branch)
// and reads as interrupted; a deadline that fired on its own reads as timeout
// (#345 — covers both the default max-runtime and a per-call timeout).
func classify(jobCtx context.Context, result *protocol.ToolCallResultV1, err error, userCancelled, swept bool) string {
	switch {
	case userCancelled:
		return StateCancelled
	case swept:
		return StateInterrupted
	case errors.Is(jobCtx.Err(), context.DeadlineExceeded):
		return StateTimeout
	case err != nil:
		return StateFailed
	case result != nil && result.IsError:
		return StateFailed
	default:
		return StateSucceeded
	}
}

// startJob opens a ringmaster job and returns its id. Per the pinned contract,
// CLOWN_DISABLE_JOB_WAKEUP=1 makes `ringmaster start` an exit-0 no-op that
// prints NOTHING — empty stdout on zero exit is the normal disabled-channel
// signature, not an error. In that case (and on any exec failure) a local
// id of the same shape is minted; async keeps working as a poll surface.
func (m *Manager) startJob(ctx context.Context, label string) string {
	cmd := exec.CommandContext(ctx, m.ringmasterBin,
		"start", "--source", "moxy", "--label", label)
	out, err := cmd.Output()
	id := strings.TrimSpace(string(out))
	if err != nil || id == "" {
		if err != nil {
			lifecyclelog.Log("asyncjob: ringmaster start (%s): %v — minting local id", label, err)
		}
		return mintID(label)
	}
	return id
}

// resolveSpoolPath asks ringmaster for the job's output spool path (RFC-0010
// §2). Empty string ("" → no spool) is the normal answer when the channel is
// disabled (CLOWN_DISABLE_JOB_WAKEUP=1: exit 0, no output) or when ringmaster
// is absent / the id was minted locally / the installed ringmaster predates the
// spool surface (any exec failure). The native exec layer skips the tee on
// an empty path, so async degrades to its v1 shape — best-effort per
// FDR-0005.
func (m *Manager) resolveSpoolPath(ctx context.Context, id string) string {
	cmd := exec.CommandContext(ctx, m.ringmasterBin, "spool-path", id)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// JobStatus shells `ringmaster status <id> --json` and returns the parsed
// object (RFC-0010 §3: state, source, started, ended, elapsed_sec,
// last_activity, spool_bytes, progress, tail). An error means ringmaster
// couldn't derive a status — channel disabled, ringmaster absent, a
// locally-minted id with no journal (exit 1), or an installed ringmaster
// without the probe — and the caller (async-result) falls back to its
// in-memory v1 shape.
func (m *Manager) JobStatus(ctx context.Context, id string) (map[string]any, error) {
	cmd := exec.CommandContext(ctx, m.ringmasterBin, "status", id, "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ringmaster status %s: %w", id, err)
	}
	var status map[string]any
	if err := json.Unmarshal(out, &status); err != nil {
		return nil, fmt.Errorf("ringmaster status %s: parsing %q: %w", id, string(out), err)
	}
	return status, nil
}

// terminalRecordTypes is the set of RFC-0009 §5 wire states that mark a job's
// terminal journal record. `timeout` is intentionally absent — it is a
// moxy-only distinction emitted on the wire as `interrupted` (see wireState).
var terminalRecordTypes = map[string]bool{
	StateSucceeded:   true,
	StateFailed:      true,
	StateCancelled:   true,
	StateInterrupted: true,
}

// JournalView is the reduced view of a job's clown journal record stream that
// async-result consults. State is the wire type of the last terminal record,
// or "" when the stream carries no terminal record yet — which means the job
// is still running and is NEVER inferred dead (RFC-0010 §3: status is
// journal-derived and cannot detect a producer that died without emitting a
// terminal). ResultRef is that terminal record's machine-readable artifact URI
// (a `madder://blobs/<digest>`), empty for a no-result terminal. Found is false
// when the stream yielded no parseable records.
type JournalView struct {
	State     string
	ResultRef string
	Message   string
	Started   time.Time
	Ended     time.Time
	Found     bool
}

// journalRecord mirrors the subset of the RFC-0009 record schema
// ({v,job,session,source,from,type,seq,ts,message,result_ref}) that
// ReadJournal consumes.
type journalRecord struct {
	Type      string `json:"type"`
	TS        string `json:"ts"`
	Message   string `json:"message"`
	ResultRef string `json:"result_ref"`
}

// ReadJournal shells `ringmaster read --job <id> --json` and reduces the job's
// record stream (NDJSON, one RFC-0009 record per line) to a JournalView. The
// `--job` selector scopes the read to one job's full stream (mirroring the
// job_read MCP `job` arg) rather than the channel firehose. The last terminal
// record is authoritative for state and result_ref.
//
// An error means the journal is unavailable — the channel is disabled
// (CLOWN_DISABLE_JOB_WAKEUP=1, clown's canonical kill switch), ringmaster is
// absent, or the id was minted locally and has no journal — and the caller
// (async-result) falls back to the in-process snapshot. The error contract
// mirrors JobStatus.
func (m *Manager) ReadJournal(ctx context.Context, id string) (JournalView, error) {
	cmd := exec.CommandContext(ctx, m.ringmasterBin, "read", "--job", id, "--json")
	out, err := cmd.Output()
	if err != nil {
		return JournalView{}, fmt.Errorf("ringmaster read %s: %w", id, err)
	}

	var view JournalView
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec journalRecord
		if json.Unmarshal(line, &rec) != nil {
			continue // tolerate a stray non-record line
		}
		view.Found = true
		ts := parseJournalTime(rec.TS)
		if view.Started.IsZero() && !ts.IsZero() {
			view.Started = ts
		}
		if terminalRecordTypes[rec.Type] {
			view.State = rec.Type
			view.ResultRef = rec.ResultRef
			view.Message = rec.Message
			view.Ended = ts
		}
	}
	if err := sc.Err(); err != nil {
		return JournalView{}, fmt.Errorf("ringmaster read %s: scanning: %w", id, err)
	}
	return view, nil
}

// parseJournalTime parses an RFC 3339 journal timestamp, returning the zero
// time on any error (an absent/garbled ts is not fatal to a journal read).
func parseJournalTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// emitDone reports the terminal state. The state is not repeated in the
// message text (the wake line renders it from the record's state field);
// the digest rides in the message so results survive a moxy restart.
//
// result_ref carries the MACHINE-READABLE artifact reference — the
// `madder://blobs/<digest>` URI — so a journal reader (this or another moxy
// process, or `ringmaster read`) can recover the result from the terminal
// record alone (#321). It is kept strictly to the artifact URI; a terminal
// with no result (a cancelled/interrupted job that produced none) carries no
// ref, and human nuance like a timeout note lives only in the message.
func (m *Manager) emitDone(tool, id, state, summary, digest string) {
	message := tool + ":"
	if summary != "" {
		message += " " + summary
	}
	if digest != "" {
		message += " (madder " + digest + ")"
	}
	args := []string{
		"done", id,
		"--state", wireState(state),
		"--message", message,
	}
	if digest != "" {
		args = append(args, "--result-ref", "madder://blobs/"+digest)
	}
	cmd := exec.Command(m.ringmasterBin, args...)
	if err := cmd.Run(); err != nil {
		lifecyclelog.Log("asyncjob: ringmaster done %s: %v", id, err)
	}
}

// mintID produces a clown-shaped id (<label>-<8hex>) from the job-id
// charset [A-Za-z0-9._-]; clown's own sanitizer keeps dots, so ours does
// too.
func mintID(label string) string {
	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		b.WriteString("job")
	}
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return b.String() + "-" + hex.EncodeToString(buf[:])
}

// firstLine extracts a one-line summary from a tool result's first
// text-bearing block, truncated for the wake line. Native moxin results
// with a content-type arrive as embedded-resource blocks whose text lives
// in Resource.Text rather than the top-level Text field.
func firstLine(result *protocol.ToolCallResultV1) string {
	for _, block := range result.Content {
		if block.Text != "" {
			return firstLineOf(block.Text)
		}
		if block.Resource != nil && block.Resource.Text != nil && *block.Resource.Text != "" {
			return firstLineOf(*block.Resource.Text)
		}
	}
	return ""
}

const summaryMax = 120

// summaryBannerPrefixes match the header lines formatSummary (in
// internal/native/cache.go) prepends to a madder-cached result: a
// truncation warning, a blob-URI pointer, and a line count. None of them
// are the tool's actual output, so a wake-line summary skips past them to
// the first real content line.
var summaryBannerPrefixes = []string{"⚠ TRUNCATED", "Full output:", "Lines:"}

func firstLineOf(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line == "" || isSummaryBannerLine(line) {
			continue
		}
		return truncateSummary(line)
	}
	return ""
}

func isSummaryBannerLine(line string) bool {
	for _, prefix := range summaryBannerPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func truncateSummary(s string) string {
	if len(s) > summaryMax {
		s = s[:summaryMax] + "…"
	}
	return s
}
