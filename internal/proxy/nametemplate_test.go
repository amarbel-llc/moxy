package proxy

import (
	"testing"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"github.com/amarbel-llc/moxy/internal/naming"
	"github.com/amarbel-llc/moxy/internal/toolfilter"
)

func TestMapCategory(t *testing.T) {
	cases := []struct {
		in   naming.Category
		want toolfilter.Category
	}{
		{naming.CategoryChild, toolfilter.Child},
		{naming.CategoryResourceBridge, toolfilter.ResourceBridge},
		{naming.CategoryMeta, toolfilter.Meta},
	}
	for _, c := range cases {
		if got := mapCategory(c.in); got != c.want {
			t.Errorf("mapCategory(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestCategoryFromRegistryCarried is the load-bearing property: under a custom
// template the rendered name no longer reveals its category, so the category
// must come from the registry, not from re-parsing the name. A dot-free
// "madder-mcp_resource-read" would be misclassified Meta by toolfilter.Categorize
// (no dot ⇒ builtin), but the registry knows it is a ResourceBridge.
func TestCategoryFromRegistryCarried(t *testing.T) {
	tmpl, _ := naming.Parse("{server}_{tool}")
	b := naming.NewBuilder(tmpl)
	b.Add(naming.Entry{Server: "grit", Original: "status", Kind: naming.KindTool, Category: naming.CategoryChild})
	b.Add(naming.Entry{Server: "madder-mcp", Original: "resource-read", Kind: naming.KindTool, Category: naming.CategoryResourceBridge})
	reg, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if got := categoryFromRegistry("grit_status", reg); got != toolfilter.Child {
		t.Errorf("grit_status category = %v, want Child", got)
	}
	if got := categoryFromRegistry("madder-mcp_resource-read", reg); got != toolfilter.ResourceBridge {
		t.Errorf("madder-mcp_resource-read category = %v, want ResourceBridge (registry-carried, not name-parsed)", got)
	}
	// A builtin (no registry entry, no dot) falls back to Categorize ⇒ Meta.
	if got := categoryFromRegistry("restart", reg); got != toolfilter.Meta {
		t.Errorf("restart category = %v, want Meta", got)
	}
}

// TestApplyToolFilterCustomTemplate proves the --expose filter still gates
// correctly when names are rendered with a dot-free template: the resource-bridge
// tool is dropped under -resource-bridge despite its name being indistinguishable
// from a child tool by parsing alone.
func TestApplyToolFilterCustomTemplate(t *testing.T) {
	tmpl, _ := naming.Parse("{server}_{tool}")
	b := naming.NewBuilder(tmpl)
	b.Add(naming.Entry{Server: "grit", Original: "status", Kind: naming.KindTool, Category: naming.CategoryChild})
	b.Add(naming.Entry{Server: "madder-mcp", Original: "resource-read", Kind: naming.KindTool, Category: naming.CategoryResourceBridge})
	reg, _ := b.Build()

	f, _ := toolfilter.Parse("-resource-bridge")
	p := &Proxy{toolFilter: f, nameTemplate: tmpl}
	tools := []protocol.ToolV1{
		{Name: "grit_status"},
		{Name: "madder-mcp_resource-read"},
	}
	got := p.applyToolFilter(append([]protocol.ToolV1(nil), tools...), reg)
	if len(got) != 1 || got[0].Name != "grit_status" {
		t.Errorf("applyToolFilter under -resource-bridge = %+v, want only grit_status", got)
	}
}

// TestApplyToolFilterDefaultTemplateResourceReadEdge locks in the FDR-0006
// edge under the *default* template: a child's OWN tool literally named
// resource-read is classified ResourceBridge (by name), so --expose
// -resource-bridge drops it. Under the default template applyToolFilter must
// classify by name (toolfilter.Categorize), NOT by the registry — where that
// tool is recorded as a plain Child and would otherwise survive the filter.
// This test distinguishes the IsDefault-branch implementation from a
// registry-only one (which would wrongly keep server.resource-read).
func TestApplyToolFilterDefaultTemplateResourceReadEdge(t *testing.T) {
	// Registry as ListToolsV1 builds it under the default template: a child's
	// own resource-read tool is a normal Child entry (it was added in the
	// child-tools loop, not as a synthetic bridge).
	b := naming.NewBuilder(naming.DefaultTemplate())
	b.Add(naming.Entry{Server: "srv", Original: "resource-read", Kind: naming.KindTool, Category: naming.CategoryChild})
	b.Add(naming.Entry{Server: "srv", Original: "foo", Kind: naming.KindTool, Category: naming.CategoryChild})
	reg, _ := b.Build()

	f, _ := toolfilter.Parse("-resource-bridge")
	p := &Proxy{toolFilter: f, nameTemplate: naming.DefaultTemplate()}
	tools := []protocol.ToolV1{
		{Name: "srv.resource-read"},
		{Name: "srv.foo"},
	}
	got := p.applyToolFilter(append([]protocol.ToolV1(nil), tools...), reg)
	if len(got) != 1 || got[0].Name != "srv.foo" {
		t.Errorf("applyToolFilter under default template + -resource-bridge = %+v, want only srv.foo (srv.resource-read is ResourceBridge by name, FDR-0006 edge)", got)
	}
}
