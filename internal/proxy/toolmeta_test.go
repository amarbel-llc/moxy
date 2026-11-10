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

// assertMetaHas checks every key of want is in got with an equal value. moxy
// adds its own moxy/ keys to every tool (moxy#517), so a tool's whole _meta is
// no longer only what the moxin or child declared.
func assertMetaHas(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var g, w map[string]any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got is not a JSON object (%q): %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not a JSON object (%q): %v", want, err)
	}
	for k, wv := range w {
		gb, _ := json.Marshal(g[k])
		wb, _ := json.Marshal(wv)
		if string(gb) != string(wb) {
			t.Errorf("_meta[%q] = %s, want %s (whole _meta %s)", k, gb, wb, got)
		}
	}
}

// metaDeclares reports whether a tool's _meta carries any key other than
// moxy's own canonical ones, that is, something the moxin or child declared.
func metaDeclares(t *testing.T, raw json.RawMessage) bool {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("_meta %q: %v", raw, err)
	}
	for k := range m {
		if k != "moxy/server" && k != "moxy/tool" && k != "moxy/perms-request" {
			return true
		}
	}
	return false
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
	assertMetaHas(t, got, `{"kind":"list"}`)
	if raw, ok := metas["fixture.plain"]; ok && metaDeclares(t, raw) {
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
	assertMetaHas(t, got, childMeta)
	if raw, ok := metas["srv.bare"]; ok && metaDeclares(t, raw) {
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
	assertMetaHas(t, got, childMeta)
}

// #454: listing replays the ephemeral cache, and prefixing the server name onto
// a replayed tool's titles must not write through to the cached annotations.
func TestListToolsV1EphemeralTitlePrefixDoesNotStack(t *testing.T) {
	tools, err := decodeToolsList(json.RawMessage(
		`{"tools":[{"name":"find","title":"Find","inputSchema":{"type":"object"},` +
			`"annotations":{"title":"Find things"}}]}`,
	))
	if err != nil {
		t.Fatalf("decodeToolsList: %v", err)
	}
	p := &Proxy{ephemeral: map[string]*EphemeralMeta{
		"eph": {Capabilities: toolsCapable(), Tools: tools},
	}}

	for i := range 2 {
		result, err := p.ListToolsV1(context.Background(), "")
		if err != nil {
			t.Fatalf("listing %d: ListToolsV1: %v", i+1, err)
		}
		var found bool
		for _, tool := range result.Tools {
			if tool.Name != "eph.find" {
				continue
			}
			found = true
			if tool.Title != "eph: Find" {
				t.Errorf("listing %d: Title = %q, want %q", i+1, tool.Title, "eph: Find")
			}
			if tool.Annotations == nil || tool.Annotations.Title != "eph: Find things" {
				t.Errorf("listing %d: Annotations = %+v, want title %q", i+1, tool.Annotations, "eph: Find things")
			}
		}
		if !found {
			t.Fatalf("listing %d: eph.find not listed", i+1)
		}
	}
	if got := tools[0].Annotations.Title; got != "Find things" {
		t.Errorf("cached annotations title = %q, want it untouched", got)
	}
}

// The tools moxy makes up itself (a failed server's status tool, the resource
// bridge) declare nothing of their own, so their `_meta` holds only the
// canonical server and tool moxy adds to every tool (moxy#517); no kind, no
// perms-request.
func TestListToolsV1SyntheticToolsCarryOnlyCanonicalMeta(t *testing.T) {
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

	for name, raw := range listedToolMeta(t, p) {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("_meta of %s: %v", name, err)
		}
		for k := range m {
			if k != "moxy/server" && k != "moxy/tool" {
				t.Errorf("synthetic tool %s carries _meta key %q: %v", name, k, m)
			}
		}
	}
}
