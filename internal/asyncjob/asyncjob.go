// Package asyncjob backgrounds moxy tool dispatches: each job runs on a
// detached context, writes its full result through a pluggable writer
// (production: a user-level madder store), and reports terminal states over
// clown's job-wakeup channel by shelling out to ${CLOWN_BIN:-clown}. See
// docs/features/0004-async-tool-dispatch.md for the design and the pinned
// producer contract (RFC-0009).
package asyncjob

import (
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

// Options configures a Manager.
type Options struct {
	// ClownBin is the clown binary to shell out to. Empty falls back to
	// $CLOWN_BIN, then bare "clown" (PATH lookup at exec time).
	ClownBin string
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

	clownBin    string
	writeResult WriteResultFunc
	maxRuntime  time.Duration
	sem         chan struct{}
}

// New builds a Manager from Options.
func New(opts Options) *Manager {
	bin := opts.ClownBin
	if bin == "" {
		bin = os.Getenv("CLOWN_BIN")
	}
	if bin == "" {
		bin = "clown"
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
		jobs:        make(map[string]*job),
		clownBin:    bin,
		writeResult: opts.WriteResult,
		maxRuntime:  maxRuntime,
		sem:         make(chan struct{}, concurrency),
	}
}

// Dispatch opens a clown job (or mints a local id when the channel is
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

// startJob opens a clown job and returns its id. Per the pinned contract,
// CLOWN_DISABLE_JOB_WAKEUP=1 makes `clown job start` an exit-0 no-op that
// prints NOTHING — empty stdout on zero exit is the normal disabled-channel
// signature, not an error. In that case (and on any exec failure) a local
// id of the same shape is minted; async keeps working as a poll surface.
func (m *Manager) startJob(ctx context.Context, label string) string {
	cmd := exec.CommandContext(ctx, m.clownBin,
		"job", "start", "--source", "moxy", "--label", label)
	out, err := cmd.Output()
	id := strings.TrimSpace(string(out))
	if err != nil || id == "" {
		if err != nil {
			lifecyclelog.Log("asyncjob: clown job start (%s): %v — minting local id", label, err)
		}
		return mintID(label)
	}
	return id
}

// emitDone reports the terminal state. The state is not repeated in the
// message text (the wake line renders it from the record's state field);
// the digest rides in the message so results survive a moxy restart.
func (m *Manager) emitDone(tool, id, state, summary, digest string) {
	message := tool + ":"
	if summary != "" {
		message += " " + summary
	}
	if digest != "" {
		message += " (madder " + digest + ")"
	}
	cmd := exec.Command(m.clownBin,
		"job", "done", id,
		"--state", wireState(state),
		"--message", message,
		"--result-ref", "moxy async-result "+id)
	if err := cmd.Run(); err != nil {
		lifecyclelog.Log("asyncjob: clown job done %s: %v", id, err)
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

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > summaryMax {
		s = s[:summaryMax] + "…"
	}
	return s
}
