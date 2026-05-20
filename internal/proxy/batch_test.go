package proxy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"github.com/amarbel-llc/moxy/internal/native"
	"github.com/amarbel-llc/moxy/internal/permcheck"
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
	p := newProxyWithResolverAndDispatch(t,
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
