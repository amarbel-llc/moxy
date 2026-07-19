package proxy

import (
	"context"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"

	"code.linenisgreat.com/moxy/internal/naming"
	"code.linenisgreat.com/moxy/internal/toolfilter"
)

func toolNameSet(tools []protocol.ToolV1) map[string]bool {
	m := make(map[string]bool, len(tools))
	for _, t := range tools {
		m[t.Name] = true
	}
	return m
}

func TestApplyToolFilter(t *testing.T) {
	base := []protocol.ToolV1{
		{Name: "grit.status"},
		{Name: "madder-mcp.resource-read"},
		{Name: "cutting-garden.resource-templates"},
		{Name: "restart"},
		{Name: "status"},
	}
	cases := []struct {
		spec string
		want []string
	}{
		{"", []string{"grit.status", "madder-mcp.resource-read", "cutting-garden.resource-templates", "restart", "status"}},
		{"full", []string{"grit.status", "madder-mcp.resource-read", "cutting-garden.resource-templates", "restart", "status"}},
		{"resources-only", []string{}},
		{"no-meta", []string{"grit.status", "madder-mcp.resource-read", "cutting-garden.resource-templates"}},
		{"-resource-bridge", []string{"grit.status", "restart", "status"}},
	}
	for _, c := range cases {
		f, err := toolfilter.Parse(c.spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.spec, err)
		}
		p := &Proxy{toolFilter: f, nameTemplate: naming.DefaultTemplate()}
		cp := append([]protocol.ToolV1(nil), base...)
		got := toolNameSet(p.applyToolFilter(cp, naming.Registry{}))
		if len(got) != len(c.want) {
			t.Errorf("spec %q: got %d tools %v, want %d %v", c.spec, len(got), got, len(c.want), c.want)
		}
		for _, n := range c.want {
			if !got[n] {
				t.Errorf("spec %q: expected %q to survive filter", c.spec, n)
			}
		}
	}
}

// A filtered category must be uncallable, not merely unlisted — this is the
// security boundary for a public --expose origin.
func TestCallToolFilterGate(t *testing.T) {
	f, _ := toolfilter.Parse("resources-only")
	p := &Proxy{toolFilter: f, nameTemplate: naming.DefaultTemplate()}

	for _, name := range []string{"restart", "grit.status", "madder-mcp.resource-read"} {
		res, err := p.callToolV1(context.Background(), name, nil)
		if err != nil {
			t.Fatalf("callToolV1(%q) unexpected error: %v", name, err)
		}
		if res == nil || !res.IsError {
			t.Errorf("callToolV1(%q) under resources-only: expected error result, got %+v", name, res)
		}
	}
}
