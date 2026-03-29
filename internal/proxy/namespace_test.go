package proxy

import "testing"

// TestDotSeparatorRouting verifies that splitPrefix with "." correctly splits
// namespaced tool/prompt names into server name and original name.
func TestDotSeparatorRouting(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantServer string
		wantTool   string
	}{
		{
			name:       "simple server, hyphenated tool",
			input:      "grit.commit-changes",
			wantServer: "grit",
			wantTool:   "commit-changes",
		},
		{
			name:       "simple server, underscore tool",
			input:      "chix.flake_check",
			wantServer: "chix",
			wantTool:   "flake_check",
		},
		{
			name:       "simple server, no separator in tool",
			input:      "grit.status",
			wantServer: "grit",
			wantTool:   "status",
		},
		{
			name:       "hyphenated server, hyphenated tool",
			input:      "just-us-agents.list-recipes",
			wantServer: "just-us-agents",
			wantTool:   "list-recipes",
		},
		{
			name:       "hyphenated server, underscore tool",
			input:      "my-server.resource_read",
			wantServer: "my-server",
			wantTool:   "resource_read",
		},
		{
			name:       "mixed tool name preserved exactly",
			input:      "srv.get-foo_bar",
			wantServer: "srv",
			wantTool:   "get-foo_bar",
		},
		{
			name:       "tool with dot in name (split on first dot)",
			input:      "srv.admin.tools.list",
			wantServer: "srv",
			wantTool:   "admin.tools.list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, tool, ok := splitPrefix(tt.input, ".")
			if !ok {
				t.Fatalf("splitPrefix(%q) returned ok=false", tt.input)
			}
			if server != tt.wantServer {
				t.Errorf("server: got %q, want %q", server, tt.wantServer)
			}
			if tool != tt.wantTool {
				t.Errorf("tool: got %q, want %q", tool, tt.wantTool)
			}
		})
	}
}

// TestDotSeparatorRoundTrip verifies the full namespace -> route cycle:
// the tool name the child receives must exactly match what it originally
// advertised.
func TestDotSeparatorRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		serverName string
		toolName   string
	}{
		{"hyphenated tool", "grit", "commit-changes"},
		{"underscore tool", "chix", "flake_check"},
		{"mixed tool", "srv", "get-foo_bar"},
		{"multi-underscore tool", "chix", "interactive_rebase_plan"},
		{"hyphenated server, hyphenated tool", "just-us-agents", "list-recipes"},
		{"hyphenated server, underscore tool", "my-server", "resource_read"},
		{"plain tool", "srv", "status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate ListToolsV1: namespace the tool name
			namespaced := tt.serverName + "." + tt.toolName

			// Simulate CallToolV1: recover server and tool
			gotServer, gotTool, ok := splitPrefix(namespaced, ".")
			if !ok {
				t.Fatalf("splitPrefix(%q) returned ok=false", namespaced)
			}

			if gotServer != tt.serverName {
				t.Errorf("server: got %q, want %q (namespaced=%q)", gotServer, tt.serverName, namespaced)
			}
			// The key property: tool name is preserved exactly.
			if gotTool != tt.toolName {
				t.Errorf("tool: got %q, want %q (namespaced=%q)", gotTool, tt.toolName, namespaced)
			}
		})
	}
}

// TestDotSeparatorNoCollision verifies that distinct (server, tool) pairs
// never produce the same namespaced string.
func TestDotSeparatorNoCollision(t *testing.T) {
	type pair struct {
		server, tool string
	}

	pairs := []pair{
		{"my-server", "read"},
		{"my", "server.read"},
		{"a-b", "c"},
		{"a", "b.c"},
		{"srv", "foo-bar"},
		{"srv", "foo_bar"},
	}

	seen := make(map[string]pair)
	for _, p := range pairs {
		namespaced := p.server + "." + p.tool
		if prev, ok := seen[namespaced]; ok {
			t.Errorf(
				"collision: %q produced by both (%q, %q) and (%q, %q)",
				namespaced, prev.server, prev.tool, p.server, p.tool,
			)
		}
		seen[namespaced] = p
	}
}
