package permsmatrix

import (
	"strings"
	"testing"

	"code.linenisgreat.com/moxy/internal/native"
)

func tool(name string, tier native.PermsRequest, props ...string) native.ToolSpec {
	in := &native.InputSchema{Type: "object", Properties: map[string]native.PropertySchema{}}
	for _, p := range props {
		in.Properties[p] = native.PropertySchema{Type: "string"}
	}
	return native.ToolSpec{Name: name, PermsRequest: tier, InputParsed: in}
}

func TestUngatedPathReaderNeedsAlwaysAllowNoResourcesAndAPathInput(t *testing.T) {
	gated := tool("gated", native.PermsDynamic, "files")
	gated.Resources = []native.ResourceArg{{Param: "files", Op: "read"}}
	cfg := &native.NativeConfig{Name: "srv", Tools: []native.ToolSpec{
		tool("open", native.PermsAlwaysAllow, "path"),
		tool("clock", native.PermsAlwaysAllow, "timezone"),
		tool("asks", native.PermsEachUse, "path"),
		gated,
	}}
	var flagged []string
	for _, r := range Build([]*native.NativeConfig{cfg}) {
		if r.UngatedPathReader() {
			flagged = append(flagged, r.Tool)
		}
	}
	if len(flagged) != 1 || flagged[0] != "open" {
		t.Fatalf("flagged = %v, want [open]", flagged)
	}
}

func TestGateNamesWhoJudgesTheInputs(t *testing.T) {
	cases := map[string]string{
		"always-allow":       "none",
		"dynamic":            "floor",
		"each-use":           "prompt",
		"delegate-to-client": "client",
		"":                   "client",
	}
	for tier, want := range cases {
		r := rowOf("s", &native.ToolSpec{Name: "t", PermsRequest: native.PermsRequest(tier)})
		if got := r.Gate(); got != want {
			t.Errorf("tier %q: gate = %q, want %q", tier, got, want)
		}
	}
	withRes := native.ToolSpec{
		Name: "t", PermsRequest: native.PermsDynamic,
		Resources: []native.ResourceArg{{Param: "p", Op: "read", Grammar: "toolbox"}},
	}
	r := rowOf("s", &withRes)
	if r.Gate() != "broker+floor" || r.Resources[0] != "p:read+toolbox" {
		t.Fatalf("got gate %q resources %v", r.Gate(), r.Resources)
	}
}

func TestRenderIsDeterministicAndListsEveryTool(t *testing.T) {
	cfg := &native.NativeConfig{Name: "b", Tools: []native.ToolSpec{
		tool("z", native.PermsAlwaysAllow), tool("a", native.PermsAlwaysAllow, "cwd"),
	}}
	cfg2 := &native.NativeConfig{Name: "a", Tools: []native.ToolSpec{tool("m", native.PermsEachUse)}}
	out := Render(Build([]*native.NativeConfig{cfg, cfg2}))
	if out != Render(Build([]*native.NativeConfig{cfg2, cfg})) {
		t.Fatal("render depends on config order")
	}
	for _, want := range []string{"`a.m`", "`b.a`", "`b.z`", "3 tools: 2 always-allow"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if !strings.Contains(out, "- `b.a`: cwd") {
		t.Errorf("b.a not flagged:\n%s", out)
	}
}
