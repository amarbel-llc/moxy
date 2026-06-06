package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"github.com/amarbel-llc/moxy/internal/asyncjob"
	"github.com/amarbel-llc/moxy/internal/permcheck"
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
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

type asyncJobRef struct {
	JobID string `json:"job_id"`
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

	dec, reason := p.resolver.Resolve(ctx, params.Tool, params.Args, ".")
	if dec != permcheck.Allow {
		return protocol.ErrorResultV1(fmt.Sprintf(
			"async requires the call to resolve to allow; %s resolved to %s (%s)",
			params.Tool, dec, reason,
		)), nil
	}

	id, err := p.asyncManager.Dispatch(ctx, params.Tool, params.Args,
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
// stored result.
func (p *Proxy) HandleAsyncResult(
	ctx context.Context,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	snap, errResult := p.asyncLookup(args)
	if errResult != nil {
		return errResult, nil
	}

	if snap.State == asyncjob.StateRunning {
		return asyncJSONResult(map[string]any{
			"job_id":  snap.ID,
			"tool":    snap.Tool,
			"status":  snap.State,
			"started": snap.Started.Format(time.RFC3339),
		}), nil
	}

	// Terminal with a stored result: hand back the original result
	// verbatim (IsError and all).
	if snap.Result != nil {
		return snap.Result, nil
	}

	// Terminal without a result (cancelled/interrupted before producing
	// one): report the terminal state.
	return asyncJSONResult(map[string]any{
		"job_id":   snap.ID,
		"tool":     snap.Tool,
		"status":   snap.State,
		"summary":  snap.Summary,
		"finished": snap.Finished.Format(time.RFC3339),
	}), nil
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

	return asyncJSONResult(map[string]any{
		"job_id": snap.ID,
		"status": snap.State,
	}), nil
}

// asyncLookup parses {job_id} args and resolves the job, returning a
// structured error result (never a Go error) on bad input or unknown ids.
func (p *Proxy) asyncLookup(args json.RawMessage) (asyncjob.Snapshot, *protocol.ToolCallResultV1) {
	if p.asyncManager == nil {
		return asyncjob.Snapshot{}, protocol.ErrorResultV1(
			"async unavailable: no job manager configured",
		)
	}
	var ref asyncJobRef
	if err := json.Unmarshal(args, &ref); err != nil {
		return asyncjob.Snapshot{}, protocol.ErrorResultV1(
			fmt.Sprintf("invalid args: %v", err),
		)
	}
	if ref.JobID == "" {
		return asyncjob.Snapshot{}, protocol.ErrorResultV1("job_id is required")
	}
	snap, ok := p.asyncManager.Lookup(ref.JobID)
	if !ok {
		ids := p.asyncManager.IDs()
		sort.Strings(ids)
		hint := "no async jobs known to this moxy process"
		if len(ids) > 0 {
			hint = "known job ids: " + strings.Join(ids, ", ")
		}
		return asyncjob.Snapshot{}, protocol.ErrorResultV1(fmt.Sprintf(
			"unknown async job %q (%s)", ref.JobID, hint,
		))
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
