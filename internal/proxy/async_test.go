package proxy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
	"code.linenisgreat.com/purse-first/libs/go-mcp/server"

	"code.linenisgreat.com/moxy/internal/asyncjob"
	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/internal/permcheck"
)

// newTestAsyncManager builds an asyncjob.Manager suitable for these
// preflight tests: clown bin absent (ids are minted locally), fake result
// writer, generous runtime. Shared so every test wiring an async manager
// doesn't hand-repeat the same options struct.
func newTestAsyncManager() *asyncjob.Manager {
	return asyncjob.New(asyncjob.Options{
		RingmasterBin: "/nonexistent/ringmaster",
		WriteResult: func(_ context.Context, _ []byte) (string, error) {
			return "fake-digest", nil
		},
		MaxRuntime: time.Minute,
	})
}

// newAsyncProxy builds a Proxy with an allow-listed fake tool and an async
// manager whose clown bin is absent (ids are minted locally) and whose
// result writer is a fake — the handler contract under test is identical.
func newAsyncProxy(t *testing.T) *Proxy {
	t.Helper()
	p := &Proxy{}
	p.SetResolver(permcheck.NewResolverWithPerms(map[string]permcheck.ToolPermInfo{
		"fake.tool": {Perm: native.PermsAlwaysAllow},
	}))
	p.SetAsyncManager(newTestAsyncManager())
	return p
}

func TestHandleAsyncDispatchesAllowedTool(t *testing.T) {
	p := newAsyncProxy(t)

	result, err := p.HandleAsync(context.Background(),
		json.RawMessage(`{"tool":"fake.tool","args":{}}`))
	if err != nil {
		t.Fatalf("HandleAsync: %v", err)
	}
	if result.IsError {
		t.Fatalf("HandleAsync returned error result: %+v", result)
	}
	var ref struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &ref); err != nil {
		t.Fatalf("parsing handle: %v", err)
	}
	if ref.Status != "running" || ref.JobID == "" {
		t.Fatalf("handle = %+v, want running with job id", ref)
	}

	// fake.tool isn't a real child, so the dispatch resolves to the
	// unknown-server error result → terminal state failed, with the full
	// error result stored and returned by async-result.
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := p.HandleAsyncResult(context.Background(),
			json.RawMessage(`{"job_id":"`+ref.JobID+`"}`))
		if err != nil {
			t.Fatalf("HandleAsyncResult: %v", err)
		}
		if res.IsError {
			// Terminal: the stored result IS the original (IsError) result.
			if !strings.Contains(res.Content[0].Text, "unknown server") {
				t.Fatalf("stored result = %+v", res)
			}
			break
		}
		if strings.Contains(res.Content[0].Text, `"running"`) {
			if time.Now().After(deadline) {
				t.Fatal("job never reached terminal state")
			}
			time.Sleep(5 * time.Millisecond)
			continue
		}
		t.Fatalf("unexpected async-result payload: %+v", res)
	}

	// Cancel on a terminal job is a no-op reporting the terminal state.
	res, err := p.HandleAsyncCancel(context.Background(),
		json.RawMessage(`{"job_id":"`+ref.JobID+`"}`))
	if err != nil {
		t.Fatalf("HandleAsyncCancel: %v", err)
	}
	if !strings.Contains(res.Content[0].Text, `"failed"`) {
		t.Errorf("cancel on terminal job = %+v, want failed state report", res)
	}
}

// FDR 0011: Unknown (no perms-request) and ask inner tools are ADMITTED by the
// async preflight — the PreToolUse hook forces an at-dispatch consent before
// the call reaches moxy, so the preflight trusts the hook (moxy's core model)
// and backgrounds them. This reverses FDR 0004's allow-only posture for these
// two cases; deny and permit-async=false remain hard rejects (see below).
func TestHandleAsyncAdmitsUnknownAndAskTools(t *testing.T) {
	p := &Proxy{}
	p.SetResolver(permcheck.NewResolverWithPerms(map[string]permcheck.ToolPermInfo{
		"ask.tool": {Perm: native.PermsEachUse},
		// unknown.tool is deliberately absent → Resolve returns Unknown.
	}))
	p.SetAsyncManager(newTestAsyncManager())

	for _, tool := range []string{"unknown.tool", "ask.tool"} {
		result, err := p.HandleAsync(context.Background(),
			json.RawMessage(`{"tool":"`+tool+`","args":{}}`))
		if err != nil {
			t.Fatalf("HandleAsync(%s): %v", tool, err)
		}
		if result.IsError {
			t.Fatalf("HandleAsync(%s) rejected, want admitted: %+v", tool, result)
		}
		var ref struct {
			JobID  string `json:"job_id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(result.Content[0].Text), &ref); err != nil {
			t.Fatalf("parsing handle for %s: %v", tool, err)
		}
		if ref.Status != "running" || ref.JobID == "" {
			t.Fatalf("handle(%s) = %+v, want running with job id", tool, ref)
		}
	}
}

func TestHandleBatchAsyncRunsAsOneJob(t *testing.T) {
	okResult := &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "ok"}},
	}
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			"fake.tool": {Perm: native.PermsAlwaysAllow},
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return okResult, nil
		},
	)
	p.SetAsyncManager(newTestAsyncManager())

	result, err := p.HandleBatch(context.Background(), json.RawMessage(
		`{"async":true,"calls":[{"tool":"fake.tool","args":{}},{"tool":"fake.tool","args":{}}]}`,
	))
	if err != nil {
		t.Fatalf("HandleBatch async: %v", err)
	}
	if result.IsError {
		t.Fatalf("async batch rejected: %+v", result)
	}
	var ref struct {
		JobID  string `json:"job_id"`
		Tool   string `json:"tool"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &ref); err != nil {
		t.Fatalf("parsing handle: %v", err)
	}
	if ref.Tool != "batch" || ref.Status != "running" {
		t.Fatalf("handle = %+v", ref)
	}

	// The detached run is the byte-identical synchronous batch: wait for
	// the stored TAP-NDJSON result.
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, err := p.HandleAsyncResult(context.Background(),
			json.RawMessage(`{"job_id":"`+ref.JobID+`"}`))
		if err != nil {
			t.Fatalf("HandleAsyncResult: %v", err)
		}
		text := res.Content[0].Text
		if strings.Contains(text, `"running"`) {
			if time.Now().After(deadline) {
				t.Fatal("async batch never finished")
			}
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if !strings.Contains(text, `"type":"summary"`) ||
			!strings.Contains(text, `"passed":2`) {
			t.Fatalf("stored batch result = %q", text)
		}
		break
	}
}

// #404 (FDR 0011 batch parity): Ask-tier and Unknown (no perms-request)
// sub-calls are ADMITTED by the async batch preflight, mirroring bare
// async's relaxation — the PreToolUse hook forces one consent covering the
// whole `calls` list before the batch reaches moxy (tryBatchAsyncInnerDecision
// in internal/hook), so the preflight trusts the hook and backgrounds them.
// Only Deny remains a hard synchronous reject (see
// TestHandleBatchAsyncRejectsDenyTier).
func TestHandleBatchAsyncAdmitsAskAndUnknownTiers(t *testing.T) {
	okResult := &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "ok"}},
	}
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			"ask.tool": {Perm: native.PermsEachUse},
			// unknown.tool is deliberately absent → Resolve returns Unknown.
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return okResult, nil
		},
	)
	p.SetAsyncManager(newTestAsyncManager())

	result, err := p.HandleBatch(context.Background(), json.RawMessage(
		`{"async":true,"calls":[{"tool":"ask.tool","args":{}},{"tool":"unknown.tool","args":{}}]}`,
	))
	if err != nil {
		t.Fatalf("HandleBatch async: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected admitted (backgrounded), got bailout: %+v", result)
	}
	var ref struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &ref); err != nil {
		t.Fatalf("parsing handle: %v", err)
	}
	if ref.Status != "running" || ref.JobID == "" {
		t.Fatalf("handle = %+v, want running with job id", ref)
	}
}

// Deny remains an absolute synchronous reject for async batches, unlike
// Ask/Unknown — once granted, a deny cannot be overridden by any consent.
func TestHandleBatchAsyncRejectsDenyTier(t *testing.T) {
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			"deny.tool": {Perm: native.PermsDynamic, DynamicPerms: &native.DynamicPermsSpec{
				Command: "sh",
				Args:    []string{"-c", "exit 2"},
			}},
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			t.Fatal("dispatch must not run for a rejected async batch")
			return nil, nil
		},
	)
	p.SetAsyncManager(newTestAsyncManager())

	result, err := p.HandleBatch(context.Background(), json.RawMessage(
		`{"async":true,"calls":[{"tool":"deny.tool","args":{}}]}`,
	))
	if err != nil {
		t.Fatalf("HandleBatch async: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected bailout for denied sub-call, got %+v", result)
	}
}

// permit-async = false forbids backgrounding even at allow tier (#317) —
// orthogonal to the permission gate, with a distinct rejection text.
func TestHandleAsyncRejectsPermitAsyncFalse(t *testing.T) {
	noAsync := false
	p := &Proxy{}
	p.SetResolver(permcheck.NewResolverWithPerms(map[string]permcheck.ToolPermInfo{
		"fake.tool": {Perm: native.PermsAlwaysAllow, PermitAsync: &noAsync},
	}))
	p.SetAsyncManager(newTestAsyncManager())

	result, err := p.HandleAsync(context.Background(),
		json.RawMessage(`{"tool":"fake.tool","args":{}}`))
	if err != nil {
		t.Fatalf("HandleAsync: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected rejection, got %+v", result)
	}
	if !strings.Contains(result.Content[0].Text, "permit-async = false") {
		t.Errorf("rejection text = %q, want permit-async mention", result.Content[0].Text)
	}

	// Same gate for batch async: the sub-call is allow-tier but forbidden,
	// and the dispatcher must never run.
	p.dispatchSubCall = func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
		t.Fatal("dispatch must not run for a permit-async = false sub-call")
		return nil, nil
	}
	result, err = p.HandleBatch(context.Background(), json.RawMessage(
		`{"async":true,"calls":[{"tool":"fake.tool","args":{}}]}`,
	))
	if err != nil {
		t.Fatalf("HandleBatch async: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected bailout, got %+v", result)
	}
	if !strings.Contains(result.Content[0].Text, "permit-async = false") {
		t.Errorf("bailout text = %q, want permit-async mention", result.Content[0].Text)
	}
}

// registerBuiltins wires a minimal builtin-tool registry so hasBuiltinTool
// recognizes the named meta tools, mirroring cmd/moxy's registration.
func registerBuiltins(t *testing.T, p *Proxy, names ...string) {
	t.Helper()
	reg := server.NewToolRegistryV1()
	noop := func(context.Context, json.RawMessage) (*protocol.ToolCallResultV1, error) {
		return &protocol.ToolCallResultV1{}, nil
	}
	for _, n := range names {
		reg.Register(protocol.ToolV1{Name: n, InputSchema: json.RawMessage(`{"type":"object"}`)}, noop)
	}
	p.SetBuiltinTools(reg)
}

// async {tool: "restart"} must be refused explicitly as a builtin, not by
// the accidental Unknown-perm path (#333).
func TestHandleAsyncRejectsBuiltinTool(t *testing.T) {
	p := newAsyncProxy(t)
	registerBuiltins(t, p, "restart", "batch", "async")

	result, err := p.HandleAsync(context.Background(),
		json.RawMessage(`{"tool":"restart","args":{}}`))
	if err != nil {
		t.Fatalf("HandleAsync: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected rejection, got %+v", result)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "builtin meta tool") || !strings.Contains(text, "restart") {
		t.Errorf("rejection text = %q, want builtin refusal citing restart", text)
	}
}

// batch {async:true} with a builtin sub-call (async batch-of-batch) must
// bail out citing the builtin, and never dispatch (#333).
func TestHandleBatchAsyncRejectsBuiltinSubCall(t *testing.T) {
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			"fake.tool": {Perm: native.PermsAlwaysAllow},
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			t.Fatal("dispatch must not run for a builtin sub-call")
			return nil, nil
		},
	)
	registerBuiltins(t, p, "restart", "batch", "async")
	p.SetAsyncManager(newTestAsyncManager())

	result, err := p.HandleBatch(context.Background(), json.RawMessage(
		`{"async":true,"calls":[{"tool":"fake.tool","args":{}},{"tool":"batch","args":{}}]}`,
	))
	if err != nil {
		t.Fatalf("HandleBatch async: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected bailout, got %+v", result)
	}
	if !strings.Contains(result.Content[0].Text, "builtin meta tool") {
		t.Errorf("bailout text = %q, want builtin refusal", result.Content[0].Text)
	}
}

// Pins asyncEligible's exact contract directly, independent of HandleAsync/
// HandleBatch's own wiring — the cheapest place to catch a future edit that
// changes what "async-eligible" means for one classification but not
// another.
func TestAsyncEligible(t *testing.T) {
	noAsync := false
	p := newProxyWithResolverAndDispatch(t, map[string]permcheck.ToolPermInfo{
		"allow.tool":   {Perm: native.PermsAlwaysAllow},
		"ask.tool":     {Perm: native.PermsEachUse},
		"noasync.tool": {Perm: native.PermsAlwaysAllow, PermitAsync: &noAsync},
		"deny.tool": {Perm: native.PermsDynamic, DynamicPerms: &native.DynamicPermsSpec{
			Command: "sh", Args: []string{"-c", "exit 2"},
		}},
		// unknown.tool is deliberately absent → Resolve returns Unknown.
	}, nil)
	registerBuiltins(t, p, "restart", "batch", "async")

	for _, tc := range []struct {
		tool     string
		wantKind asyncRejectKind
	}{
		{"allow.tool", asyncRejectNone},
		{"ask.tool", asyncRejectNone},
		{"unknown.tool", asyncRejectNone},
		{"deny.tool", asyncRejectDeny},
		{"noasync.tool", asyncRejectPermitAsyncFalse},
		{"restart", asyncRejectBuiltin},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			kind, _, _ := p.asyncEligible(context.Background(), tc.tool, json.RawMessage(`{}`))
			if kind != tc.wantKind {
				t.Fatalf("asyncEligible(%s) kind = %v, want %v", tc.tool, kind, tc.wantKind)
			}
		})
	}
}

// Resilience guard for the asyncEligible consolidation: HandleAsync (single
// tool) and handleBatchAsync (one-item batch) must NEVER disagree on
// whether a tool is eligible for backgrounding. Before the consolidation
// this held only because the same 3-check policy (builtin / deny /
// permit-async=false) happened to be hand-duplicated identically in both
// places — exactly the kind of duplication that drifted apart once already
// (a DIFFERENT pair of independently-coded preflights, HandleBatch's sync
// gate vs handleBatchAsync's, disagreed and caused a real bug). This test
// pins the invariant so a future edit to either HandleAsync or
// handleBatchAsync alone, without updating the shared asyncEligible, is
// caught immediately instead of silently reintroducing that class of bug.
func TestAsyncEligible_HandleAsyncAndHandleBatchAsyncAgree(t *testing.T) {
	noAsync := false
	perms := map[string]permcheck.ToolPermInfo{
		"allow.tool":   {Perm: native.PermsAlwaysAllow},
		"ask.tool":     {Perm: native.PermsEachUse},
		"noasync.tool": {Perm: native.PermsAlwaysAllow, PermitAsync: &noAsync},
		"deny.tool": {Perm: native.PermsDynamic, DynamicPerms: &native.DynamicPermsSpec{
			Command: "sh", Args: []string{"-c", "exit 2"},
		}},
		// unknown.tool is deliberately absent from perms → Resolve is Unknown.
	}
	okResult := &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "ok"}},
	}

	for _, tc := range []struct {
		name        string
		tool        string
		wantBuiltin bool
	}{
		{"allow", "allow.tool", false},
		{"ask", "ask.tool", false},
		{"unknown", "unknown.tool", false},
		{"deny", "deny.tool", false},
		{"permit-async-false", "noasync.tool", false},
		{"builtin", "restart", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pSingle := newProxyWithResolverAndDispatch(t, perms,
				func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
					return okResult, nil
				})
			pBatch := newProxyWithResolverAndDispatch(t, perms,
				func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
					return okResult, nil
				})
			if tc.wantBuiltin {
				registerBuiltins(t, pSingle, "restart", "batch", "async")
				registerBuiltins(t, pBatch, "restart", "batch", "async")
			}
			for _, p := range []*Proxy{pSingle, pBatch} {
				p.SetAsyncManager(newTestAsyncManager())
			}

			singleResult, err := pSingle.HandleAsync(context.Background(),
				json.RawMessage(`{"tool":"`+tc.tool+`","args":{}}`))
			if err != nil {
				t.Fatalf("HandleAsync(%s): %v", tc.tool, err)
			}
			batchResult, err := pBatch.HandleBatch(context.Background(), json.RawMessage(
				`{"async":true,"calls":[{"tool":"`+tc.tool+`","args":{}}]}`,
			))
			if err != nil {
				t.Fatalf("HandleBatch async(%s): %v", tc.tool, err)
			}

			if singleResult.IsError != batchResult.IsError {
				t.Fatalf("%s: HandleAsync admitted=%v but handleBatchAsync admitted=%v (single=%q, batch=%q)",
					tc.tool, !singleResult.IsError, !batchResult.IsError,
					singleResult.Content[0].Text, batchResult.Content[0].Text)
			}
		})
	}
}

func TestHandleAsyncValidation(t *testing.T) {
	p := newAsyncProxy(t)

	result, _ := p.HandleAsync(context.Background(), json.RawMessage(`{}`))
	if !result.IsError || !strings.Contains(result.Content[0].Text, "tool is required") {
		t.Errorf("missing tool: %+v", result)
	}

	// An unparseable / non-positive timeout is rejected synchronously (#345).
	result, _ = p.HandleAsync(context.Background(),
		json.RawMessage(`{"tool":"fake.tool","args":{},"timeout":"soon"}`))
	if !result.IsError || !strings.Contains(result.Content[0].Text, "invalid async timeout") {
		t.Errorf("bad timeout: %+v", result)
	}

	result, _ = p.HandleAsyncResult(context.Background(), json.RawMessage(`{"job_id":"nope-123"}`))
	if !result.IsError || !strings.Contains(result.Content[0].Text, "unknown async job") {
		t.Errorf("unknown id: %+v", result)
	}

	result, _ = p.HandleAsyncCancel(context.Background(), json.RawMessage(`{}`))
	if !result.IsError || !strings.Contains(result.Content[0].Text, "job_id is required") {
		t.Errorf("missing job_id: %+v", result)
	}
}
