package proxy

import (
	"context"
	"encoding/json"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/jsonrpc"
	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/native"
)

// toolsListBackend is a child server that answers tools/list with a fixed
// JSON document, so a test controls the bytes a real child would send.
type toolsListBackend struct {
	name      string
	toolsList string
}

func (b *toolsListBackend) Call(context.Context, string, any) (json.RawMessage, error) {
	return json.RawMessage(b.toolsList), nil
}
func (b *toolsListBackend) Notify(string, any) error                 { return nil }
func (b *toolsListBackend) SetOnNotification(func(*jsonrpc.Message)) {}
func (b *toolsListBackend) Name() string                             { return b.name }
func (b *toolsListBackend) Close() error                             { return nil }

// listedToolMeta lists the proxy's tools, round-trips the result through JSON
// the way a client receives it, and returns each tool's raw `_meta` (absent
// from the map when the tool carries none).
func listedToolMeta(t *testing.T, p *Proxy) map[string]json.RawMessage {
	t.Helper()
	result, err := p.ListToolsV1(context.Background(), "")
	if err != nil {
		t.Fatalf("ListToolsV1: %v", err)
	}
	wire, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed struct {
		Tools []struct {
			Name string          `json:"name"`
			Meta json.RawMessage `json:"_meta"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(wire, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	metas := map[string]json.RawMessage{}
	for _, tool := range parsed.Tools {
		if tool.Meta != nil {
			metas[tool.Name] = tool.Meta
		}
	}
	return metas
}

func assertJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got is not JSON (%q): %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not JSON (%q): %v", want, err)
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Errorf("_meta = %s, want %s", gb, wb)
	}
}

func toolsCapable() protocol.ServerCapabilitiesV1 {
	return protocol.ServerCapabilitiesV1{Tools: &protocol.ToolsCapability{}}
}

// #451: a moxin tool's `kind` reaches the client as `_meta.kind`, and a tool
// that declares none carries no `_meta` at all.
func TestListToolsV1EmitsMoxinKindAsMeta(t *testing.T) {
	child := native.NewServer(&native.NativeConfig{
		Name: "fixture",
		Tools: []native.ToolSpec{
			{Name: "list-things", Command: "echo", Kind: "list"},
			{Name: "plain", Command: "echo"},
		},
	})
	p := &Proxy{children: []ChildEntry{{Client: child, Capabilities: toolsCapable()}}}

	metas := listedToolMeta(t, p)
	got, ok := metas["fixture.list-things"]
	if !ok {
		t.Fatalf("fixture.list-things has no _meta; metas: %v", metas)
	}
	assertJSONEqual(t, got, `{"kind":"list"}`)
	if raw, ok := metas["fixture.plain"]; ok {
		t.Errorf("fixture.plain declared no kind but carries _meta %s", raw)
	}
}

// #451: a child server's own `_meta` passes through whole, not just `kind`.
func TestListToolsV1PassesChildMetaThrough(t *testing.T) {
	const childMeta = `{"kind":"issue","vendor/extra":{"n":1,"tags":["a","b"]},"flag":true}`
	child := &toolsListBackend{
		name: "srv",
		toolsList: `{"tools":[
			{"name":"create","inputSchema":{"type":"object"},"_meta":` + childMeta + `},
			{"name":"bare","inputSchema":{"type":"object"}}
		]}`,
	}
	p := &Proxy{children: []ChildEntry{{Client: child, Capabilities: toolsCapable()}}}

	metas := listedToolMeta(t, p)
	got, ok := metas["srv.create"]
	if !ok {
		t.Fatalf("srv.create has no _meta; metas: %v", metas)
	}
	assertJSONEqual(t, got, childMeta)
	if raw, ok := metas["srv.bare"]; ok {
		t.Errorf("srv.bare sent no _meta but carries %s", raw)
	}
}

// An ephemeral server's tools are cached at probe time and replayed on every
// list; the cache must keep `_meta` too.
func TestListToolsV1PassesEphemeralChildMetaThrough(t *testing.T) {
	const childMeta = `{"kind":"list","vendor/extra":"x"}`
	tools, err := decodeToolsList(json.RawMessage(
		`{"tools":[{"name":"find","inputSchema":{"type":"object"},"_meta":` + childMeta + `}]}`,
	))
	if err != nil {
		t.Fatalf("decodeToolsList: %v", err)
	}
	p := &Proxy{ephemeral: map[string]*EphemeralMeta{
		"eph": {Capabilities: toolsCapable(), Tools: tools},
	}}

	metas := listedToolMeta(t, p)
	got, ok := metas["eph.find"]
	if !ok {
		t.Fatalf("eph.find has no _meta; metas: %v", metas)
	}
	assertJSONEqual(t, got, childMeta)
}

// The tools moxy makes up itself (a failed server's status tool, the resource
// bridge) declare nothing, so they carry no `_meta`.
func TestListToolsV1SyntheticToolsCarryNoMeta(t *testing.T) {
	child := &toolsListBackend{name: "srv", toolsList: `{"tools":[]}`}
	caps := toolsCapable()
	caps.Resources = &protocol.ResourcesCapability{}
	p := &Proxy{
		children: []ChildEntry{{Client: child, Capabilities: caps}},
		failed:   []FailedServer{{Name: "down", Error: "boom"}},
	}

	result, err := p.ListToolsV1(context.Background(), "")
	if err != nil {
		t.Fatalf("ListToolsV1: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"down.status", "srv.resource-read", "srv.resource-templates"} {
		if !names[want] {
			t.Fatalf("expected synthetic tool %q in %v", want, names)
		}
	}

	metas := listedToolMeta(t, p)
	if len(metas) != 0 {
		t.Errorf("synthetic tools carry _meta: %v", metas)
	}
}
