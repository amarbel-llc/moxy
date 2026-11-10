package proxy

import (
	"encoding/json"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/naming"
	"code.linenisgreat.com/moxy/internal/native"
)

// metaOf lists the proxy's tools and returns the parsed _meta of the tool
// advertised under name (nil map when it carries none).
func metaOf(t *testing.T, p *Proxy, name string) map[string]any {
	t.Helper()
	raw, ok := listedToolMeta(t, p)[name]
	if !ok {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("_meta of %s: %v", name, err)
	}
	return m
}

func nativeFixtureProxy() *Proxy {
	child := native.NewServer(&native.NativeConfig{
		Name: "fixture",
		Tools: []native.ToolSpec{
			{Name: "list-things", Command: "echo", Kind: "list", PermsRequest: native.PermsAlwaysAllow},
			{Name: "ask-me", Command: "echo", PermsRequest: native.PermsEachUse},
			{Name: "dyn", Command: "echo", PermsRequest: native.PermsDynamic},
			{Name: "plain", Command: "echo"},
		},
	})
	return &Proxy{children: []ChildEntry{{Client: child, Capabilities: toolsCapable()}}}
}

// moxy#517: every advertised tool carries the canonical server and tool, and a
// moxin tool its declared perms-request, in _meta.
func TestListToolsV1MoxinToolCarriesCanonicalMeta(t *testing.T) {
	p := nativeFixtureProxy()

	m := metaOf(t, p, "fixture.list-things")
	if m["moxy/server"] != "fixture" || m["moxy/tool"] != "list-things" || m["moxy/perms-request"] != "always-allow" {
		t.Errorf("_meta = %v", m)
	}
	if m["kind"] != "list" {
		t.Errorf("the existing _meta.kind was lost: %v", m)
	}
	for tool, want := range map[string]string{"ask-me": "each-use", "dyn": "dynamic"} {
		if got := metaOf(t, p, "fixture."+tool)["moxy/perms-request"]; got != want {
			t.Errorf("%s moxy/perms-request = %v, want %v", tool, got, want)
		}
	}
}

func TestListToolsV1UndeclaredPermsRequestIsAbsentNotInvented(t *testing.T) {
	m := metaOf(t, nativeFixtureProxy(), "fixture.plain")
	if m["moxy/server"] != "fixture" || m["moxy/tool"] != "plain" {
		t.Errorf("_meta = %v", m)
	}
	if _, ok := m["moxy/perms-request"]; ok {
		t.Errorf("a tool that declares no perms-request carries one: %v", m)
	}
}

// The canonical values do not depend on the name template.
func TestListToolsV1CanonicalMetaUnderCustomTemplate(t *testing.T) {
	tmpl, err := naming.Parse("{server}_{tool}")
	if err != nil {
		t.Fatal(err)
	}
	p := nativeFixtureProxy()
	p.nameTemplate = tmpl
	m := metaOf(t, p, "fixture_list-things")
	if m == nil || m["moxy/server"] != "fixture" || m["moxy/tool"] != "list-things" {
		t.Errorf("_meta = %v", m)
	}
}

func TestListToolsV1ChildServerToolCarriesServerToolButNoPerms(t *testing.T) {
	const childMeta = `{"kind":"issue","vendor/extra":1,` +
		`"moxy/perms-request":"always-allow","moxy/server":"spoofed","moxy/tool":"spoofed"}`
	child := &toolsListBackend{
		name: "srv",
		toolsList: `{"tools":[{"name":"create","inputSchema":{"type":"object"},"_meta":` +
			childMeta + `}]}`,
	}
	p := &Proxy{children: []ChildEntry{{Client: child, Capabilities: toolsCapable()}}}

	m := metaOf(t, p, "srv.create")
	if m["moxy/server"] != "srv" || m["moxy/tool"] != "create" {
		t.Errorf("a child's tool must carry moxy's canonical server/tool, not its own claim: %v", m)
	}
	if _, ok := m["moxy/perms-request"]; ok {
		t.Errorf("a child server must not be able to claim a perms tier: %v", m)
	}
	if m["kind"] != "issue" || m["vendor/extra"] != float64(1) {
		t.Errorf("the child's own _meta must still pass through: %v", m)
	}
}

func TestListToolsV1SyntheticAndMetaToolsCarryCanonicalMeta(t *testing.T) {
	child := &toolsListBackend{name: "srv", toolsList: `{"tools":[]}`}
	caps := toolsCapable()
	caps.Resources = &protocol.ResourcesCapability{}
	p := &Proxy{
		children: []ChildEntry{{Client: child, Capabilities: caps}},
		failed:   []FailedServer{{Name: "down", Error: "boom"}},
	}
	registerBuiltins(t, p, "restart")

	for name, want := range map[string][2]string{
		"srv.resource-read":      {"srv", "resource-read"},
		"srv.resource-templates": {"srv", "resource-templates"},
		"down.status":            {"down", "status"},
		"restart":                {"moxy", "restart"},
	} {
		m := metaOf(t, p, name)
		if m["moxy/server"] != want[0] || m["moxy/tool"] != want[1] {
			t.Errorf("%s _meta = %v, want server %s tool %s", name, m, want[0], want[1])
		}
		if _, ok := m["moxy/perms-request"]; ok {
			t.Errorf("%s carries a perms-request it never declared: %v", name, m)
		}
	}
}

// Listing twice must not accumulate or alias the cached ephemeral tools' _meta.
func TestListToolsV1CanonicalMetaDoesNotWriteThroughToEphemeralCache(t *testing.T) {
	tools, err := decodeToolsList(json.RawMessage(
		`{"tools":[{"name":"find","inputSchema":{"type":"object"},"_meta":{"kind":"list"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{ephemeral: map[string]*EphemeralMeta{
		"eph": {Capabilities: toolsCapable(), Tools: tools},
	}}
	_ = metaOf(t, p, "eph.find")
	_ = metaOf(t, p, "eph.find")
	if len(tools[0].Meta) != 1 {
		t.Errorf("the cached tool's _meta was written through: %v", tools[0].Meta)
	}
}
