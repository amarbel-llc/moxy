package proxy

import "testing"

func TestSplitPrefixHyphenatedServerName(t *testing.T) {
	// Server "just-us-agents" exposes tool "list-recipes".
	// After snobcase the proxied name is "just-us-agents-list_recipes".
	// splitPrefix must recover server="just-us-agents", tool="list_recipes"
	// so that findChild can route to the correct server.

	tests := []struct {
		name       string
		input      string
		wantServer string
		wantTool   string
	}{
		{
			name:       "simple server name",
			input:      "grit-status",
			wantServer: "grit",
			wantTool:   "status",
		},
		{
			name:       "hyphenated server, simple tool",
			input:      "just-us-agents-list_recipes",
			wantServer: "just-us-agents",
			wantTool:   "list_recipes",
		},
		{
			name:       "hyphenated server, multi-word tool",
			input:      "my-server-resource_read",
			wantServer: "my-server",
			wantTool:   "resource_read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, tool, ok := splitLastPrefix(tt.input, "-")
			if !ok {
				t.Fatalf("splitPrefix(%q) returned ok=false", tt.input)
			}
			if server != tt.wantServer {
				t.Errorf(
					"splitPrefix(%q) server = %q, want %q",
					tt.input, server, tt.wantServer,
				)
			}
			if tool != tt.wantTool {
				t.Errorf(
					"splitPrefix(%q) tool = %q, want %q",
					tt.input, tool, tt.wantTool,
				)
			}
		})
	}
}

func TestToSnobCase(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"execute-command", "execute_command"},
		{"status", "status"},
		{"resource-read", "resource_read"},
		{"a-b-c", "a_b_c"},
		{"already_snake", "already_snake"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := toSnobCase(tt.input); got != tt.want {
				t.Errorf("toSnobCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestFromSnobCase(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"execute_command", "execute-command"},
		{"status", "status"},
		{"resource_read", "resource-read"},
		{"a_b_c", "a-b-c"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := fromSnobCase(tt.input); got != tt.want {
				t.Errorf("fromSnobCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
