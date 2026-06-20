package naming

import (
	"errors"
	"testing"
)

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"unknown placeholder", "{svr}_{tool}"},
		{"missing tool", "{server}"},
		{"missing tool plain literal", "static-name"},
		{"unterminated brace", "{server.{tool}"},
		{"unterminated trailing", "{server}_{tool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.raw); err == nil {
				t.Fatalf("Parse(%q): expected error, got nil", tt.raw)
			}
		})
	}
}

func TestParseValid(t *testing.T) {
	for _, raw := range []string{"", "{server}.{tool}", "{server}_{tool}", "{tool}", "{tool}.{server}", "mox_{server}__{tool}"} {
		if _, err := Parse(raw); err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", raw, err)
		}
	}
}

// TestDefaultRenderMatchesDotJoin is the back-compat guard: the default template
// must render byte-identically to the historical server+"."+tool join.
func TestDefaultRenderMatchesDotJoin(t *testing.T) {
	tmpl := DefaultTemplate()
	pairs := []struct{ server, tool string }{
		{"grit", "commit-changes"},
		{"chix", "flake_check"},
		{"just-us-agents", "list-recipes"},
		{"my-server", "resource_read"},
		{"srv", "get-foo_bar"},
		{"madder-mcp", "resource-read"},
		{"nebulous", "status"},
	}
	for _, p := range pairs {
		got := tmpl.Render(p.server, p.tool)
		want := p.server + "." + p.tool
		if got != want {
			t.Errorf("default Render(%q,%q) = %q, want %q", p.server, p.tool, got, want)
		}
	}
}

func TestRenderTemplates(t *testing.T) {
	tests := []struct {
		raw          string
		server, tool string
		want         string
	}{
		{"{server}_{tool}", "grit", "commit", "grit_commit"},
		{"{server}_{tool}", "just-us-agents", "list-recipes", "just-us-agents_list-recipes"},
		{"{tool}", "grit", "commit", "commit"},
		{"{tool}.{server}", "grit", "commit", "commit.grit"},
		{"mox_{server}__{tool}", "grit", "commit", "mox_grit__commit"},
	}
	for _, tt := range tests {
		tmpl, err := Parse(tt.raw)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.raw, err)
		}
		if got := tmpl.Render(tt.server, tt.tool); got != tt.want {
			t.Errorf("Parse(%q).Render(%q,%q) = %q, want %q", tt.raw, tt.server, tt.tool, got, tt.want)
		}
	}
}

func TestBuilderRoundTrip(t *testing.T) {
	tmpl, _ := Parse("{server}_{tool}")
	b := NewBuilder(tmpl)
	entries := []Entry{
		{Server: "grit", Original: "commit", Kind: KindTool, Category: CategoryChild},
		{Server: "madder-mcp", Original: "resource-read", Kind: KindTool, Category: CategoryResourceBridge},
		{Server: "nebulous", Original: "status", Kind: KindTool, Category: CategoryChild},
	}
	for _, e := range entries {
		b.Add(e)
	}
	reg, err := b.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, e := range entries {
		name := tmpl.Render(e.Server, e.Original)
		got, ok := reg.Lookup(name)
		if !ok {
			t.Errorf("Lookup(%q): not found", name)
			continue
		}
		if got != e {
			t.Errorf("Lookup(%q) = %+v, want %+v", name, got, e)
		}
	}
}

func TestBuilderCollisionDropServer(t *testing.T) {
	// {tool} alone: two servers exposing "read" collide.
	tmpl, _ := Parse("{tool}")
	b := NewBuilder(tmpl)
	b.Add(Entry{Server: "madder-mcp", Original: "read", Kind: KindTool})
	b.Add(Entry{Server: "cutting-garden", Original: "read", Kind: KindTool})
	_, err := b.Build()
	var ce *CollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("Build: expected *CollisionError, got %v", err)
	}
	if ce.Rendered != "read" {
		t.Errorf("collision Rendered = %q, want %q", ce.Rendered, "read")
	}
	// Deterministic ordering: A is the first-added pair.
	if ce.A.Server != "madder-mcp" || ce.B.Server != "cutting-garden" {
		t.Errorf("collision pairs not in insertion order: A=%q B=%q", ce.A.Server, ce.B.Server)
	}
}

func TestBuilderCollisionAmbiguousSeparator(t *testing.T) {
	// {server}_{tool}: (a, b_c) and (a_b, c) both render "a_b_c".
	tmpl, _ := Parse("{server}_{tool}")
	b := NewBuilder(tmpl)
	b.Add(Entry{Server: "a", Original: "b_c", Kind: KindTool})
	b.Add(Entry{Server: "a_b", Original: "c", Kind: KindTool})
	_, err := b.Build()
	var ce *CollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("Build: expected *CollisionError, got %v", err)
	}
	if ce.Rendered != "a_b_c" {
		t.Errorf("collision Rendered = %q, want %q", ce.Rendered, "a_b_c")
	}
}

func TestBuilderNoCollision(t *testing.T) {
	tmpl, _ := Parse("{server}.{tool}")
	b := NewBuilder(tmpl)
	for _, e := range []Entry{
		{Server: "my-server", Original: "read"},
		{Server: "my", Original: "server.read"},
		{Server: "srv", Original: "foo-bar"},
		{Server: "srv", Original: "foo_bar"},
	} {
		b.Add(e)
	}
	if _, err := b.Build(); err != nil {
		t.Fatalf("Build: unexpected collision: %v", err)
	}
}

func TestResolveInvertible(t *testing.T) {
	tests := []struct {
		raw                  string
		rendered             string
		wantServer, wantTool string
	}{
		{"{server}.{tool}", "grit.commit-changes", "grit", "commit-changes"},
		{"{server}_{tool}", "grit_commit", "grit", "commit"},
		{"{tool}.{server}", "commit.grit", "grit", "commit"},
	}
	for _, tt := range tests {
		tmpl, _ := Parse(tt.raw)
		if !tmpl.Invertible() {
			t.Errorf("Parse(%q).Invertible() = false, want true", tt.raw)
			continue
		}
		server, tool, ok := tmpl.Resolve(tt.rendered)
		if !ok {
			t.Errorf("Resolve(%q) under %q: ok=false", tt.rendered, tt.raw)
			continue
		}
		if server != tt.wantServer || tool != tt.wantTool {
			t.Errorf("Resolve(%q) under %q = (%q,%q), want (%q,%q)",
				tt.rendered, tt.raw, server, tool, tt.wantServer, tt.wantTool)
		}
	}
}

func TestResolveNotInvertible(t *testing.T) {
	for _, raw := range []string{"{tool}", "{server}{tool}"} {
		tmpl, _ := Parse(raw)
		if tmpl.Invertible() {
			t.Errorf("Parse(%q).Invertible() = true, want false", raw)
		}
		if _, _, ok := tmpl.Resolve("anything"); ok {
			t.Errorf("Resolve under non-invertible %q: ok=true, want false", raw)
		}
	}
}
