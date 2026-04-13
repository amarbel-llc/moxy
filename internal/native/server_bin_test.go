package native

import (
	"testing"
)

func TestResolveBinPlaceholder(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		sourceDir string
		want      string
	}{
		{
			name:      "resolves @BIN@ to sourceDir/bin",
			command:   "@BIN@/search",
			sourceDir: "/home/user/.local/share/moxy/moxins/grit",
			want:      "/home/user/.local/share/moxy/moxins/grit/bin/search",
		},
		{
			name:      "no placeholder unchanged",
			command:   "/nix/store/abc-grit/bin/search",
			sourceDir: "/home/user/.local/share/moxy/moxins/grit",
			want:      "/nix/store/abc-grit/bin/search",
		},
		{
			name:      "bare command unchanged",
			command:   "bash",
			sourceDir: "/some/dir",
			want:      "bash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveBinPlaceholder(tt.command, tt.sourceDir)
			if got != tt.want {
				t.Errorf("resolveBinPlaceholder(%q, %q) = %q, want %q",
					tt.command, tt.sourceDir, got, tt.want)
			}
		})
	}
}
