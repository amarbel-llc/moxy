package proxy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"github.com/amarbel-llc/moxy/internal/asyncjob"
	"github.com/amarbel-llc/moxy/internal/native"
	"github.com/amarbel-llc/moxy/internal/permcheck"
)

// newAsyncProxy builds a Proxy with an allow-listed fake tool and an async
// manager whose clown bin is absent (ids are minted locally) and whose
// result writer is a fake — the handler contract under test is identical.
func newAsyncProxy(t *testing.T) *Proxy {
	t.Helper()
	p := &Proxy{}
	p.SetResolver(permcheck.NewResolverWithPerms(map[string]permcheck.ToolPermInfo{
		"fake.tool": {Perm: native.PermsAlwaysAllow},
	}))
	p.SetAsyncManager(asyncjob.New(asyncjob.Options{
		ClownBin: "/nonexistent/clown",
		WriteResult: func(_ context.Context, _ []byte) (string, error) {
			return "fake-digest", nil
		},
		MaxRuntime: time.Minute,
	}))
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

func TestHandleAsyncRejectsNonAllowTools(t *testing.T) {
	p := newAsyncProxy(t)

	// unknown.tool has no perm entry → Unknown → must be rejected
	// synchronously: once detached there is no client to prompt.
	result, err := p.HandleAsync(context.Background(),
		json.RawMessage(`{"tool":"unknown.tool","args":{}}`))
	if err != nil {
		t.Fatalf("HandleAsync: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected rejection, got %+v", result)
	}
	if !strings.Contains(result.Content[0].Text, "resolve to allow") {
		t.Errorf("rejection text = %q", result.Content[0].Text)
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
	p.SetAsyncManager(asyncjob.New(asyncjob.Options{
		ClownBin: "/nonexistent/clown",
		WriteResult: func(_ context.Context, _ []byte) (string, error) {
			return "fake-digest", nil
		},
		MaxRuntime: time.Minute,
	}))

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

// Async batches are allow-only: Ask-tier sub-calls are admitted by the
// synchronous batch (the client's single prompt covers them) but MUST be
// rejected when backgrounding — there is no client to prompt once detached.
func TestHandleBatchAsyncRejectsAskTier(t *testing.T) {
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			"fake.tool": {Perm: native.PermsEachUse},
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			t.Fatal("dispatch must not run for a rejected async batch")
			return nil, nil
		},
	)
	p.SetAsyncManager(asyncjob.New(asyncjob.Options{
		ClownBin: "/nonexistent/clown",
		WriteResult: func(_ context.Context, _ []byte) (string, error) {
			return "fake-digest", nil
		},
		MaxRuntime: time.Minute,
	}))

	result, err := p.HandleBatch(context.Background(), json.RawMessage(
		`{"async":true,"calls":[{"tool":"fake.tool","args":{}}]}`,
	))
	if err != nil {
		t.Fatalf("HandleBatch async: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected bailout, got %+v", result)
	}
	if !strings.Contains(result.Content[0].Text, "allow-only") {
		t.Errorf("bailout text = %q, want allow-only mention", result.Content[0].Text)
	}
}

func TestHandleAsyncValidation(t *testing.T) {
	p := newAsyncProxy(t)

	result, _ := p.HandleAsync(context.Background(), json.RawMessage(`{}`))
	if !result.IsError || !strings.Contains(result.Content[0].Text, "tool is required") {
		t.Errorf("missing tool: %+v", result)
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
