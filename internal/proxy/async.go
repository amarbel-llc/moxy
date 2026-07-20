package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/asyncjob"
	"code.linenisgreat.com/moxy/internal/permcheck"
)

// SetAsyncManager wires the async job manager. Built in cmd/moxy with the
// clown producer and the user-level madder result store.
func (p *Proxy) SetAsyncManager(m *asyncjob.Manager) {
	p.asyncManager = m
}

// SweepAsyncJobs interrupts all running async jobs and waits for their
// terminal `done` emits. Call on graceful shutdown so no job is left open
// in the clown journal.
func (p *Proxy) SweepAsyncJobs() {
	if p.asyncManager != nil {
		p.asyncManager.Sweep()
	}
}

type asyncParams struct {
	Tool    string          `json:"tool"`
	Args    json.RawMessage `json:"args"`
	Timeout string          `json:"timeout"`
}

type asyncJobRef struct {
	JobID string `json:"job_id"`
}

// builtinAsyncRefusal is the rejection message for an attempt to background
// a moxy builtin meta tool (restart, batch, async, async-result,
// async-cancel). These are dispatched in-process by moxy itself, so
// backgrounding one via async is never valid — a detached restart would
// churn child servers while the agent keeps dispatching against them
// (#333). The refusal keys off hasBuiltinTool rather than relying on the
// accidental Unknown-perm rejection, which would lapse if builtins ever
// gained first-class permission entries.
func builtinAsyncRefusal(tool string) string {
	return fmt.Sprintf(
		"%s is a moxy builtin meta tool and cannot be dispatched asynchronously",
		tool,
	)
}

// HandleAsync dispatches one tool call in the background and returns a job
// handle immediately. Per FDR 0004 only calls whose permission resolves to
// ALLOW may background — once detached there is no client to prompt, so
// ask-gated, denied, and unknown tools are rejected synchronously.
func (p *Proxy) HandleAsync(
	ctx context.Context,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	var params asyncParams
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("invalid async args: %v", err),
		), nil
	}
	if params.Tool == "" {
		return protocol.ErrorResultV1("async.tool is required"), nil
	}
	if p.hasBuiltinTool(params.Tool) {
		return protocol.ErrorResultV1(builtinAsyncRefusal(params.Tool)), nil
	}
	if p.asyncManager == nil {
		return protocol.ErrorResultV1(
			"async unavailable: no job manager configured",
		), nil
	}
	if p.resolver == nil {
		return protocol.ErrorResultV1(
			"async unavailable: no permission resolver configured",
		), nil
	}

	// FDR 0011: only an explicit deny is an absolute synchronous reject here.
	// Allow backgrounds directly; ask and Unknown (no perms-request) are
	// admitted because the PreToolUse hook forces an at-dispatch consent before
	// the async call reaches moxy (moxy's core model presumes the hook is the
	// permission gate). This revises FDR 0004's allow-only posture for ask /
	// Unknown; #403 (mid-dispatch elicitation) would let moxy own the consent
	// itself and drop the reliance on the separate hook process.
	dec, reason := p.resolver.Resolve(ctx, params.Tool, params.Args, ".")
	if dec == permcheck.Deny {
		return protocol.ErrorResultV1(fmt.Sprintf(
			"async refuses a denied call; %s resolved to deny (%s)",
			params.Tool, reason,
		)), nil
	}
	if !p.resolver.PermitsAsync(params.Tool) {
		return protocol.ErrorResultV1(fmt.Sprintf(
			"%s declares permit-async = false; it cannot be dispatched asynchronously",
			params.Tool,
		)), nil
	}

	var timeout time.Duration
	if params.Timeout != "" {
		d, perr := time.ParseDuration(params.Timeout)
		if perr != nil || d <= 0 {
			return protocol.ErrorResultV1(fmt.Sprintf(
				"invalid async timeout %q: want a positive Go duration like \"10m\" or \"90s\"",
				params.Timeout,
			)), nil
		}
		timeout = d
	}

	id, err := p.asyncManager.Dispatch(ctx, params.Tool, params.Args, timeout,
		func(jobCtx context.Context, tool string, callArgs json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return p.CallToolV1(jobCtx, tool, callArgs)
		})
	if err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("async dispatch: %v", err),
		), nil
	}

	return asyncJSONResult(map[string]any{
		"job_id": id,
		"tool":   params.Tool,
		"status": asyncjob.StateRunning,
	}), nil
}

// HandleAsyncResult returns a running job's status or a terminal job's full
// stored result. It is JOURNAL-FIRST: when clown's journal is reachable it is
// the authority for state and the machine-readable result reference, so a job
// launched by ANOTHER moxy process resolves entirely from the shared journal +
// moxy-async store (#321). The in-process index is the fallback for degraded
// mode (channel disabled, clown absent, or a locally-minted id with no
// journal), and it owns the moxy-only `timeout` distinction.
func (p *Proxy) HandleAsyncResult(
	ctx context.Context,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	if p.asyncManager == nil {
		return protocol.ErrorResultV1("async unavailable: no job manager configured"), nil
	}
	jobID, errResult := parseJobRef(args)
	if errResult != nil {
		return errResult, nil
	}

	// The local snapshot (present only if THIS moxy launched the job) is
	// consulted alongside the journal: it carries the cached result, the
	// moxy-only `timeout` state, and lets us resolve the done-before-journal
	// race (finish flips local state to terminal before emitDone writes the
	// record).
	localSnap, haveLocal := p.asyncManager.Lookup(jobID)

	jv, jerr := p.asyncManager.ReadJournal(ctx, jobID)
	if jerr == nil && jv.Found {
		if jv.State == "" {
			// No terminal record yet. If our own index already finished, the
			// record write is merely in flight — trust the producer.
			if haveLocal && localSnap.State != asyncjob.StateRunning {
				return p.terminalFromSnapshot(localSnap), nil
			}
			resp := map[string]any{"job_id": jobID, "status": asyncjob.StateRunning}
			if haveLocal {
				resp["tool"] = localSnap.Tool
			}
			if !jv.Started.IsZero() {
				resp["started"] = jv.Started.Format(time.RFC3339)
			}
			p.mergeLiveStatus(ctx, resp, jobID)
			return asyncJSONResult(resp), nil
		}

		// Terminal. Upgrade the wire `interrupted` back to `timeout` for our
		// own timed-out jobs — clown's 4-state vocabulary (RFC-0009 §5) has no
		// `timeout`, so a cross-session reader correctly sees `interrupted`.
		state := jv.State
		if state == asyncjob.StateInterrupted && haveLocal && localSnap.State == asyncjob.StateTimeout {
			state = asyncjob.StateTimeout
		}

		// Recover the result blob from the journal's machine-readable
		// result_ref (a madder://blobs/<digest> URI).
		if jv.ResultRef != "" {
			if result, ferr := p.fetchAsyncResult(ctx, jv.ResultRef); ferr == nil {
				return result, nil
			}
			// Blob unreachable (reaped per clown#126, or store down): fall back
			// to a locally-cached result if we still hold one.
			if haveLocal && localSnap.Result != nil {
				return localSnap.Result, nil
			}
		}

		// Terminal with no recoverable result (cancelled/interrupted with no
		// blob, or a reaped blob we never cached): report the terminal state.
		out := map[string]any{"job_id": jobID, "status": state}
		if jv.Message != "" {
			out["summary"] = jv.Message
		}
		if !jv.Ended.IsZero() {
			out["finished"] = jv.Ended.Format(time.RFC3339)
		}
		return asyncJSONResult(out), nil
	}

	// In-process fallback (degraded mode: the journal is unavailable).
	if !haveLocal {
		return p.unknownJobError(jobID), nil
	}
	if localSnap.State == asyncjob.StateRunning {
		resp := map[string]any{
			"job_id":  localSnap.ID,
			"tool":    localSnap.Tool,
			"status":  localSnap.State,
			"started": localSnap.Started.Format(time.RFC3339),
		}
		p.mergeLiveStatus(ctx, resp, jobID)
		return asyncJSONResult(resp), nil
	}
	return p.terminalFromSnapshot(localSnap), nil
}

// mergeLiveStatus augments a running-job response with clown's journal+spool
// probe (RFC-0010 §3 / FDR-0005): elapsed_sec, last_activity, spool_bytes,
// progress, and a bounded output tail, surfaced under clown's own field names
// so an agent sees the same shape via async-result or `ringmaster status`. On
// any error (clown disabled/absent, a locally-minted id, or an installed clown
// without the probe) the response keeps its base shape — the probe is a
// façade, never a second source of truth.
func (p *Proxy) mergeLiveStatus(ctx context.Context, resp map[string]any, jobID string) {
	status, err := p.asyncManager.JobStatus(ctx, jobID)
	if err != nil {
		return
	}
	for _, f := range []string{"elapsed_sec", "last_activity", "spool_bytes", "progress", "tail"} {
		if v, ok := status[f]; ok {
			resp[f] = v
		}
	}
}

// terminalFromSnapshot renders a terminal in-process snapshot: the stored
// result verbatim (IsError and all) when present, else a terminal-state report
// for a cancelled/interrupted job that produced no result.
func (p *Proxy) terminalFromSnapshot(snap asyncjob.Snapshot) *protocol.ToolCallResultV1 {
	if snap.Result != nil {
		return snap.Result
	}
	return asyncJSONResult(map[string]any{
		"job_id":   snap.ID,
		"tool":     snap.Tool,
		"status":   snap.State,
		"summary":  snap.Summary,
		"finished": snap.Finished.Format(time.RFC3339),
	})
}

// fetchAsyncResult resolves a madder://blobs/<digest> result_ref to the stored
// ToolCallResultV1. The async manager writes each terminal result as marshaled
// JSON to the user-level moxy-async store and references it by this URI in the
// job's done record, so any reader — including another moxy process — recovers
// the result from the journal alone.
func (p *Proxy) fetchAsyncResult(ctx context.Context, resultRef string) (*protocol.ToolCallResultV1, error) {
	if p.madder == nil {
		return nil, fmt.Errorf("no madder backend configured")
	}
	if !strings.HasPrefix(resultRef, blobURIPrefix) {
		return nil, fmt.Errorf("result_ref %q is not a madder blob URI", resultRef)
	}
	digest := strings.TrimPrefix(resultRef, blobURIPrefix)
	if idx := strings.Index(digest, "?"); idx >= 0 {
		digest = digest[:idx]
	}
	if digest == "" {
		return nil, fmt.Errorf("result_ref %q has no digest segment", resultRef)
	}
	body, err := p.madder.CatBytes(ctx, digest)
	if err != nil {
		return nil, err
	}
	var result protocol.ToolCallResultV1
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decoding stored result %q: %w", digest, err)
	}
	return &result, nil
}

// HandleAsyncCancel context-cancels a running job. Cancelling an
// already-terminal job is a no-op that reports the terminal state.
func (p *Proxy) HandleAsyncCancel(
	ctx context.Context,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	snap, errResult := p.asyncLookup(args)
	if errResult != nil {
		return errResult, nil
	}

	if err := p.asyncManager.Cancel(snap.ID); err != nil {
		return protocol.ErrorResultV1(err.Error()), nil
	}

	// Most dispatches unwind within milliseconds of ctx cancel; give the
	// job a short window to reach its terminal state so the common case
	// reports "cancelled" rather than a transient "running".
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		cur, ok := p.asyncManager.Lookup(snap.ID)
		if ok && cur.State != asyncjob.StateRunning {
			snap = cur
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	out := map[string]any{
		"job_id": snap.ID,
		"status": snap.State,
	}
	// A wedged process tree (a SIGTERM-ignoring leaf) can take until the
	// kill-grace SIGKILL to exit, longer than this poll window. Tell the
	// agent the cancel landed and a terminal wakeup is still coming, so a
	// transient "running" isn't read as failure-to-act (#344).
	if snap.State == asyncjob.StateRunning {
		out["detail"] = "cancel requested; awaiting process-tree exit (SIGTERM sent, SIGKILL after grace)"
	}
	return asyncJSONResult(out), nil
}

// handleBatchAsync backgrounds a whole batch as ONE async job. Per #404
// (FDR 0011 batch parity), only an explicit deny is an absolute synchronous
// reject here — ask and Unknown (no perms-request) sub-calls are admitted
// because the PreToolUse hook forces one consent covering the whole `calls`
// list before the batch call reaches moxy (tryBatchAsyncInnerDecision in
// internal/hook), mirroring bare async's HandleAsync relaxation.
func (p *Proxy) handleBatchAsync(
	ctx context.Context,
	params batchParams,
) (*protocol.ToolCallResultV1, error) {
	if p.asyncManager == nil {
		return protocol.ErrorResultV1(
			"batch async unavailable: no job manager configured",
		), nil
	}

	var rejected []batchRejection
	for i, c := range params.Calls {
		if p.hasBuiltinTool(c.Tool) {
			rejected = append(rejected, batchRejection{
				index:  i,
				call:   c,
				dec:    permcheck.Unknown,
				reason: builtinAsyncRefusal(c.Tool),
			})
			continue
		}
		dec, reason := p.resolver.Resolve(ctx, c.Tool, c.Args, ".")
		if dec == permcheck.Deny {
			rejected = append(rejected, batchRejection{
				index:  i,
				call:   c,
				dec:    dec,
				reason: reason,
			})
			continue
		}
		if !p.resolver.PermitsAsync(c.Tool) {
			rejected = append(rejected, batchRejection{
				index:  i,
				call:   c,
				dec:    dec,
				reason: "tool declares permit-async = false",
			})
		}
	}
	if len(rejected) > 0 {
		return emitPreflightBailout(params.Calls, rejected), nil
	}

	// Re-enter HandleBatch without the async flag for the actual run, so
	// sequential execution, on_error semantics, and the TAP-NDJSON result
	// stay byte-identical to a synchronous batch.
	syncArgs, err := json.Marshal(batchParams{
		Calls:   params.Calls,
		OnError: params.OnError,
	})
	if err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("marshaling batch args: %v", err),
		), nil
	}

	// Batch async keeps the manager default runtime; per-batch timeout is not
	// exposed in v1.
	id, err := p.asyncManager.Dispatch(ctx, "batch", syncArgs, 0,
		func(jobCtx context.Context, _ string, callArgs json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return p.HandleBatch(jobCtx, callArgs)
		})
	if err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("async dispatch: %v", err),
		), nil
	}

	return asyncJSONResult(map[string]any{
		"job_id": id,
		"tool":   "batch",
		"status": asyncjob.StateRunning,
	}), nil
}

// parseJobRef extracts the {job_id} argument, returning a structured error
// result (never a Go error) on bad input. Unlike asyncLookup it does NOT
// require the id to be known to the local manager — a journal-resolvable
// cross-session id must reach the read path (#321).
func parseJobRef(args json.RawMessage) (string, *protocol.ToolCallResultV1) {
	var ref asyncJobRef
	if err := json.Unmarshal(args, &ref); err != nil {
		return "", protocol.ErrorResultV1(fmt.Sprintf("invalid args: %v", err))
	}
	if ref.JobID == "" {
		return "", protocol.ErrorResultV1("job_id is required")
	}
	return ref.JobID, nil
}

// unknownJobError builds the "no such job" result, listing the ids this moxy
// process knows so the agent can spot a typo or a cross-process id.
func (p *Proxy) unknownJobError(jobID string) *protocol.ToolCallResultV1 {
	ids := p.asyncManager.IDs()
	sort.Strings(ids)
	hint := "no async jobs known to this moxy process"
	if len(ids) > 0 {
		hint = "known job ids: " + strings.Join(ids, ", ")
	}
	return protocol.ErrorResultV1(fmt.Sprintf("unknown async job %q (%s)", jobID, hint))
}

// asyncLookup parses {job_id} args and resolves the job in the LOCAL index,
// returning a structured error result (never a Go error) on bad input or
// unknown ids. Used by HandleAsyncCancel, which is inherently process-local —
// you can only kill a process whose handle this moxy holds.
func (p *Proxy) asyncLookup(args json.RawMessage) (asyncjob.Snapshot, *protocol.ToolCallResultV1) {
	if p.asyncManager == nil {
		return asyncjob.Snapshot{}, protocol.ErrorResultV1(
			"async unavailable: no job manager configured",
		)
	}
	jobID, errResult := parseJobRef(args)
	if errResult != nil {
		return asyncjob.Snapshot{}, errResult
	}
	snap, ok := p.asyncManager.Lookup(jobID)
	if !ok {
		return asyncjob.Snapshot{}, p.unknownJobError(jobID)
	}
	return snap, nil
}

func asyncJSONResult(v map[string]any) *protocol.ToolCallResultV1 {
	data, err := json.Marshal(v)
	if err != nil {
		return protocol.ErrorResultV1(fmt.Sprintf("marshaling result: %v", err))
	}
	return &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{{
			Type:     "text",
			Text:     string(data),
			MimeType: "application/json",
		}},
	}
}
