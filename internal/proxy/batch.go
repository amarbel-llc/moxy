package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/permcheck"
)

// batchCall is one entry in the batch.calls array.
type batchCall struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// batchParams is the wire input shape for the batch builtin.
type batchParams struct {
	Calls   []batchCall `json:"calls"`
	OnError string      `json:"on_error,omitempty"`
	// Async backgrounds the whole batch as ONE async job (FDR 0004):
	// preflight runs synchronously and is allow-only, the TAP-NDJSON
	// result lands in the async result store, and the agent is woken on
	// the batch's terminal state.
	Async bool `json:"async,omitempty"`
}

// batchRejection records a sub-call that failed pre-flight permission
// resolution. Collected during pre-flight; emitted into the bailout
// record's summary diagnostics when any are present.
type batchRejection struct {
	index  int
	call   batchCall
	dec    permcheck.Decision
	reason string
}

// subCallDispatcher is the seam HandleBatch uses to invoke each
// sub-call. The default points at p.CallToolV1; tests override it.
type subCallDispatcher func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error)

// HandleBatch is the dispatch entrypoint for the `batch` builtin tool.
// See docs/plans/2026-05-20-batch-tool-design.md.
//
// Sub-call execution flow:
//  1. Parse + validate batchParams (malformed args, empty calls,
//     invalid on_error → Tier 1 error result).
//  2. Pre-flight every sub-call against p.resolver. Decisions Allow
//     and Ask proceed; Deny and Unknown are collected as
//     batchRejection entries. If any rejections accrue, emit a
//     bailout record + summary and stop.
//  3. Execute accepted sub-calls sequentially via the dispatcher
//     (p.dispatchSubCall if set, else p.CallToolV1). One TestRecord
//     per call.
//  4. Emit a summary record. Return a single ToolCallResultV1 with
//     IsError set if any sub-call failed.
//
// This task implements happy path + preflight. Task 10 adds on_error
// stop/continue and skip directives.
func (p *Proxy) HandleBatch(
	ctx context.Context,
	args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
	var params batchParams
	if err := json.Unmarshal(args, &params); err != nil {
		return protocol.ErrorResultV1(
			fmt.Sprintf("invalid batch args: %v", err),
		), nil
	}
	if len(params.Calls) == 0 {
		return protocol.ErrorResultV1("batch.calls must be non-empty"), nil
	}
	onError := params.OnError
	if onError == "" {
		onError = "stop"
	}
	if onError != "stop" && onError != "continue" {
		return protocol.ErrorResultV1(
			fmt.Sprintf(`invalid on_error %q (want "stop" or "continue")`, onError),
		), nil
	}

	if p.resolver == nil {
		return protocol.ErrorResultV1(
			"batch unavailable: no permission resolver configured",
		), nil
	}

	if params.Async {
		return p.handleBatchAsync(ctx, params)
	}

	// Pre-flight: resolve every sub-call.
	var rejected []batchRejection
	for i, c := range params.Calls {
		dec, reason := p.resolver.Resolve(ctx, c.Tool, c.Args, ".")
		if dec == permcheck.Allow || dec == permcheck.Ask {
			continue
		}
		rejected = append(rejected, batchRejection{
			index:  i,
			call:   c,
			dec:    dec,
			reason: reason,
		})
	}

	if len(rejected) > 0 {
		return emitPreflightBailout(params.Calls, rejected), nil
	}

	// Execute sub-calls sequentially.
	dispatch := p.dispatchSubCall
	if dispatch == nil {
		dispatch = p.CallToolV1
	}
	records := make([]ndjsonTestRecord, 0, len(params.Calls))
	passed, failed, skipped := 0, 0, 0
	stopped := false
	stoppedAtN := 0 // 1-indexed position of the failing call; only meaningful when stopped=true

	for i, c := range params.Calls {
		if stopped {
			skipReason := fmt.Sprintf("batch aborted: stopped at #%d", stoppedAtN)
			records = append(records, ndjsonTestRecord{
				Type:        "test",
				N:           i + 1,
				Description: c.Tool,
				OK:          false,
				Directive:   &ndjsonDirective{Kind: "skip", Reason: skipReason},
				Diagnostic:  nil,
				Subtest:     []ndjsonTestRecord{},
				Line:        i + 1,
			})
			skipped++
			continue
		}
		result, err := dispatch(ctx, c.Tool, c.Args)
		rec := buildTestRecord(i+1, c, result, err)
		records = append(records, rec)
		if rec.OK {
			passed++
		} else {
			failed++
			if onError == "stop" {
				stopped = true
				stoppedAtN = i + 1
			}
		}
	}

	summary := ndjsonSummaryRecord{
		Type:        "summary",
		Passed:      passed,
		Failed:      failed,
		Skipped:     skipped,
		Total:       len(records),
		PlanCount:   len(params.Calls),
		Bailed:      stopped,
		Valid:       true,
		Diagnostics: []ndjsonSummaryDiagnostic{},
	}
	return formatNDJSON(records, nil, summary, failed > 0 || stopped), nil
}

func buildTestRecord(n int, c batchCall, result *protocol.ToolCallResultV1, err error) ndjsonTestRecord {
	rec := ndjsonTestRecord{
		Type:        "test",
		N:           n,
		Description: c.Tool,
		Diagnostic:  map[string]any{"tool": c.Tool, "args": json.RawMessage(c.Args)},
		Subtest:     []ndjsonTestRecord{},
		Line:        n,
	}
	if err != nil {
		rec.OK = false
		rec.Diagnostic["error"] = err.Error()
		rec.Diagnostic["kind"] = "transport"
		return rec
	}
	if result != nil && result.IsError {
		out := contentToString(result.Content)
		rec.OK = false
		rec.Diagnostic["error"] = out
		rec.Diagnostic["kind"] = "tool"
		rec.Output = &out
		return rec
	}
	rec.OK = true
	if result != nil {
		out := contentToString(result.Content)
		rec.Output = &out
	}
	return rec
}

// contentToString renders a sub-call's content blocks to a string for
// the NDJSON Output field. Text blocks contribute their Text directly;
// non-text blocks (resource_link, image) get a [type] stub. The future
// madder-blob formatter (follow-up) will replace this with URI
// references and full content in the blob store.
func contentToString(blocks []protocol.ContentBlockV1) string {
	var sb bytes.Buffer
	for i, b := range blocks {
		if i > 0 {
			sb.WriteByte('\n')
		}
		if b.Type == "text" {
			sb.WriteString(b.Text)
		} else {
			fmt.Fprintf(&sb, "[%s]", b.Type)
		}
	}
	return sb.String()
}

func emitPreflightBailout(calls []batchCall, rejected []batchRejection) *protocol.ToolCallResultV1 {
	bail := ndjsonBailoutRecord{
		Type:    "bailout",
		Message: fmt.Sprintf("batch denied: %d of %d sub-calls failed pre-flight", len(rejected), len(calls)),
		Line:    0,
	}
	diags := make([]ndjsonSummaryDiagnostic, 0, len(rejected))
	for _, r := range rejected {
		diags = append(diags, ndjsonSummaryDiagnostic{
			Line:     r.index + 1,
			Severity: "error",
			Message:  fmt.Sprintf("%s: %s (%s)", r.call.Tool, r.reason, r.dec),
		})
	}
	summary := ndjsonSummaryRecord{
		Type:        "summary",
		Skipped:     len(calls),
		Total:       len(calls),
		PlanCount:   len(calls),
		Bailed:      true,
		Valid:       false,
		Diagnostics: diags,
	}
	return formatNDJSON(nil, &bail, summary, true)
}

// formatNDJSON serializes records + optional bailout + summary into
// a single text-block ToolCallResultV1. The text payload is the
// NDJSON stream — one JSON object per line, in the order:
//
//	bailout? testRecord* summary
func formatNDJSON(
	records []ndjsonTestRecord,
	bailout *ndjsonBailoutRecord,
	summary ndjsonSummaryRecord,
	isError bool,
) *protocol.ToolCallResultV1 {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if bailout != nil {
		_ = enc.Encode(bailout)
	}
	for _, r := range records {
		_ = enc.Encode(r)
	}
	_ = enc.Encode(summary)
	return &protocol.ToolCallResultV1{
		IsError: isError,
		Content: []protocol.ContentBlockV1{{Type: "text", Text: buf.String()}},
	}
}
