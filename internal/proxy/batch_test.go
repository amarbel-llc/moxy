package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/native"
	"code.linenisgreat.com/moxy/internal/permcheck"
)

// newProxyWithResolverAndDispatch builds a Proxy stub wired with a
// hand-crafted resolver and a scripted sub-call dispatcher. This is
// the test seam for HandleBatch unit tests — it skips Proxy's
// production bootstrap and the MOXIN_PATH walk.
func newProxyWithResolverAndDispatch(
	t *testing.T,
	perms map[string]permcheck.ToolPermInfo,
	dispatch func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error),
) *Proxy {
	t.Helper()
	p := &Proxy{}
	p.SetResolver(permcheck.NewResolverWithPerms(perms))
	p.dispatchSubCall = dispatch
	return p
}

func TestBatch_AllAllow_Sequential(t *testing.T) {
	okResult := &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "ok"}},
	}
	var calls []string
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			"fake.tool": {Perm: native.PermsAlwaysAllow},
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			calls = append(calls, name)
			return okResult, nil
		},
	)

	in := []byte(`{
        "calls": [
            {"tool": "fake.tool", "args": {"a": 1}},
            {"tool": "fake.tool", "args": {"a": 2}}
        ]
    }`)

	res, err := p.HandleBatch(context.Background(), in)
	if err != nil {
		t.Fatalf("HandleBatch: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected IsError=false; content=%v", res.Content)
	}
	if got, want := len(calls), 2; got != want {
		t.Fatalf("dispatch invoked %d times, want %d", got, want)
	}
	body := res.Content[0].Text
	if !strings.Contains(body, `"type":"test"`) || !strings.Contains(body, `"type":"summary"`) {
		t.Fatalf("body missing expected records: %s", body)
	}
	if !strings.Contains(body, `"passed":2`) {
		t.Fatalf("expected passed=2 in summary: %s", body)
	}
}

func TestBatch_OnErrorStop_DefaultsToStop(t *testing.T) {
	failResult := &protocol.ToolCallResultV1{
		IsError: true,
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "boom"}},
	}
	okResult := &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "ok"}},
	}
	var calls []string
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			"fake.tool": {Perm: native.PermsAlwaysAllow},
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			calls = append(calls, name)
			if len(calls) == 2 {
				return failResult, nil
			}
			return okResult, nil
		},
	)

	in := []byte(`{"calls": [
        {"tool":"fake.tool","args":{}},
        {"tool":"fake.tool","args":{}},
        {"tool":"fake.tool","args":{}}
    ]}`)

	res, err := p.HandleBatch(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
	if len(calls) != 2 {
		t.Fatalf("expected stop after 2 calls; got %d", len(calls))
	}
	body := res.Content[0].Text
	if !strings.Contains(body, `"directive":{"kind":"skip"`) {
		t.Errorf("expected skip directive on remainder; body=%s", body)
	}
	if !strings.Contains(body, `"bailed":true`) {
		t.Errorf("expected bailed=true; body=%s", body)
	}
}

func TestBatch_OnErrorStop_MultipleSkipsCorrectIndex(t *testing.T) {
	failResult := &protocol.ToolCallResultV1{
		IsError: true,
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "boom"}},
	}
	okResult := &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "ok"}},
	}
	var calls []string
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			"fake.tool": {Perm: native.PermsAlwaysAllow},
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			calls = append(calls, name)
			if len(calls) == 2 {
				return failResult, nil
			}
			return okResult, nil
		},
	)

	in := []byte(`{"calls": [
        {"tool":"fake.tool","args":{}},
        {"tool":"fake.tool","args":{}},
        {"tool":"fake.tool","args":{}},
        {"tool":"fake.tool","args":{}}
    ]}`)

	res, _ := p.HandleBatch(context.Background(), in)
	if len(calls) != 2 {
		t.Fatalf("expected stop after 2 calls; got %d", len(calls))
	}
	body := res.Content[0].Text

	// Both skipped records (calls 3 and 4) must reference the failing
	// call's 1-indexed position (#2), not their own positions.
	skipCount := strings.Count(body, `"reason":"batch aborted: stopped at #2"`)
	if skipCount != 2 {
		t.Errorf("expected 2 skip records all referencing #2; got %d. body=%s", skipCount, body)
	}

	// Skip records should have null diagnostic per the design doc.
	skipWithNullDiag := strings.Count(body, `"diagnostic":null`)
	if skipWithNullDiag < 2 {
		t.Errorf("expected at least 2 records with diagnostic:null (skipped calls); got %d. body=%s", skipWithNullDiag, body)
	}
}

func TestBatch_OnErrorContinue(t *testing.T) {
	failResult := &protocol.ToolCallResultV1{
		IsError: true,
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "boom"}},
	}
	okResult := &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{{Type: "text", Text: "ok"}},
	}
	var calls []string
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			"fake.tool": {Perm: native.PermsAlwaysAllow},
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			calls = append(calls, name)
			if len(calls) == 2 {
				return failResult, nil
			}
			return okResult, nil
		},
	)

	in := []byte(`{
        "on_error": "continue",
        "calls": [
            {"tool":"fake.tool","args":{}},
            {"tool":"fake.tool","args":{}},
            {"tool":"fake.tool","args":{}}
        ]
    }`)
	res, _ := p.HandleBatch(context.Background(), in)
	if len(calls) != 3 {
		t.Fatalf("expected all 3 calls to run; got %d", len(calls))
	}
	body := res.Content[0].Text
	if !strings.Contains(body, `"passed":2`) || !strings.Contains(body, `"failed":1`) {
		t.Errorf("expected passed=2 failed=1; body=%s", body)
	}
	if !strings.Contains(body, `"bailed":false`) {
		t.Errorf("expected bailed=false; body=%s", body)
	}
}

func TestBatch_PreflightDeny(t *testing.T) {
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			// No tools registered → every sub-call resolves Unknown
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			t.Fatalf("dispatch should not be called on preflight rejection")
			return nil, nil
		},
	)
	in := []byte(`{"calls":[{"tool":"any.tool","args":{}}]}`)
	res, _ := p.HandleBatch(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
	body := res.Content[0].Text
	if !strings.Contains(body, `"type":"bailout"`) {
		t.Errorf("expected bailout record; body=%s", body)
	}
}

func TestBatch_EmptyCallsRejected(t *testing.T) {
	p := newProxyWithResolverAndDispatch(t, nil, nil)
	res, _ := p.HandleBatch(context.Background(), []byte(`{"calls":[]}`))
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
}

func TestBatch_InvalidOnError(t *testing.T) {
	p := newProxyWithResolverAndDispatch(t, nil, nil)
	res, _ := p.HandleBatch(context.Background(), []byte(`{"on_error":"lol","calls":[{"tool":"x.y","args":{}}]}`))
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
}

func TestBatch_MalformedJSON(t *testing.T) {
	p := newProxyWithResolverAndDispatch(t, nil, nil)
	res, _ := p.HandleBatch(context.Background(), []byte(`{not json`))
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
}

// TestBatch_TransportError exercises buildTestRecord's err != nil branch
// (dispatcher returns a non-nil Go error) — surfaced as kind=transport in
// the diagnostic.
func TestBatch_TransportError(t *testing.T) {
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			"fake.tool": {Perm: native.PermsAlwaysAllow},
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return nil, errors.New("dispatch boom")
		},
	)
	in := []byte(`{"on_error":"continue","calls":[{"tool":"fake.tool","args":{}}]}`)
	res, _ := p.HandleBatch(context.Background(), in)
	if !res.IsError {
		t.Fatal("expected IsError=true")
	}
	body := res.Content[0].Text
	if !strings.Contains(body, `"kind":"transport"`) {
		t.Errorf("expected transport kind diagnostic; body=%s", body)
	}
	if !strings.Contains(body, "dispatch boom") {
		t.Errorf("expected error text in diagnostic; body=%s", body)
	}
}

// TestBatch_NonTextContent exercises contentToString's non-text branch —
// content blocks with Type != "text" should render as "[<type>]" stubs.
func TestBatch_NonTextContent(t *testing.T) {
	mixedResult := &protocol.ToolCallResultV1{
		Content: []protocol.ContentBlockV1{
			{Type: "text", Text: "hello"},
			{Type: "resource_link"},
		},
	}
	p := newProxyWithResolverAndDispatch(
		t,
		map[string]permcheck.ToolPermInfo{
			"fake.tool": {Perm: native.PermsAlwaysAllow},
		},
		func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
			return mixedResult, nil
		},
	)
	in := []byte(`{"calls":[{"tool":"fake.tool","args":{}}]}`)
	res, _ := p.HandleBatch(context.Background(), in)
	if res.IsError {
		t.Fatal("expected IsError=false")
	}
	body := res.Content[0].Text
	if !strings.Contains(body, `[resource_link]`) {
		t.Errorf("expected [resource_link] stub in output; body=%s", body)
	}
}
