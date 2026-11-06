package native

import (
	"context"
	"encoding/json"
	"testing"

	"code.linenisgreat.com/purse-first/libs/go-mcp/protocol"
)

// moxy#231: a tool process sees the directory of the moxin it belongs to as
// $MOXIN_DIR, so a project-local moxin can reach a script shipped beside its
// TOML without hardcoding an absolute path.
func TestToolProcessSeesMoxinDir(t *testing.T) {
	cases := []struct {
		name      string
		sourceDir string
		want      string
	}{
		{"a discovered moxin exports its directory", t.TempDir(), ""},
		{"a config with no directory exports nothing", "", "unset"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			want := c.want
			if want == "" {
				want = c.sourceDir
			}
			srv := NewServer(&NativeConfig{
				Name:      "test-server",
				SourceDir: c.sourceDir,
				Tools: []ToolSpec{{
					Name:    "where",
					Command: "sh",
					Args:    []string{"-c", `printf '%s' "${MOXIN_DIR-unset}"`},
				}},
			})
			raw, err := srv.Call(context.Background(), "tools/call", protocol.ToolCallParams{Name: "where"})
			if err != nil {
				t.Fatalf("Call tools/call: %v", err)
			}
			var result protocol.ToolCallResultV1
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatalf("unmarshal result: %v", err)
			}
			if len(result.Content) != 1 || result.Content[0].Text != want {
				t.Fatalf("MOXIN_DIR = %+v, want %q", result.Content, want)
			}
		})
	}
}
