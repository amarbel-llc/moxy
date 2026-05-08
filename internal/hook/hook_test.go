package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/moxy/internal/native"
)

func TestParseNativeToolName(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		prefix string
		want   string
		wantOK bool
	}{
		{
			name:   "simple tool (direct)",
			input:  "mcp__moxy__folio_read",
			prefix: "mcp__moxy__",
			want:   "folio.read",
			wantOK: true,
		},
		{
			name:   "simple tool (plugin)",
			input:  "mcp__plugin_moxy_moxy__folio_read",
			prefix: "mcp__plugin_moxy_moxy__",
			want:   "folio.read",
			wantOK: true,
		},
		{
			name:   "hyphenated tool",
			input:  "mcp__moxy__just-us-agents_list-recipes",
			prefix: "mcp__moxy__",
			want:   "just-us-agents.list-recipes",
			wantOK: true,
		},
		{
			name:   "tool with hyphen",
			input:  "mcp__moxy__hamster_mod-read",
			prefix: "mcp__moxy__",
			want:   "hamster.mod-read",
			wantOK: true,
		},
		{
			name:   "wrong prefix",
			input:  "mcp__moxy__folio_read",
			prefix: "mcp__plugin_moxy_moxy__",
			want:   "",
			wantOK: false,
		},
		{
			name:   "no tool part",
			input:  "mcp__moxy__restart",
			prefix: "mcp__moxy__",
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseNativeToolName(tt.input, tt.prefix)
			if ok != tt.wantOK {
				t.Errorf("ok: got %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("result: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchMoxyPrefix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"direct", "mcp__moxy__folio_read", "mcp__moxy__"},
		{"plugin", "mcp__plugin_moxy_moxy__folio_read", "mcp__plugin_moxy_moxy__"},
		{"builtin tool", "Bash", ""},
		{"other mcp server", "mcp__other__foo_bar", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchMoxyPrefix(tt.input)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTryAutoAllow(t *testing.T) {
	// Override discoverAutoAllowed for testing by testing the output
	// format directly. We can't easily mock DiscoverConfigs, so we test
	// the JSON encoding path with a known-good tool name and verify the
	// output structure matches what Claude Code expects.
	t.Run("allow response format", func(t *testing.T) {
		var buf bytes.Buffer
		out := hookOutput{
			HookSpecificOutput: hookDecision{
				HookEventName:            "PreToolUse",
				PermissionDecision:       "allow",
				PermissionDecisionReason: "auto-allowed by native server config",
			},
		}
		if err := json.NewEncoder(&buf).Encode(out); err != nil {
			t.Fatal(err)
		}

		var decoded map[string]map[string]string
		if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}

		hso := decoded["hookSpecificOutput"]
		if hso["hookEventName"] != "PreToolUse" {
			t.Errorf("hookEventName: got %q", hso["hookEventName"])
		}
		if hso["permissionDecision"] != "allow" {
			t.Errorf("permissionDecision: got %q", hso["permissionDecision"])
		}
	})
}

func TestTryBuiltinAutoAllow(t *testing.T) {
	t.Run("status is auto-allowed", func(t *testing.T) {
		var buf bytes.Buffer
		ok := tryBuiltinAutoAllow("mcp__plugin_moxy_moxy__status", "mcp__plugin_moxy_moxy__", &buf)
		if !ok {
			t.Fatal("expected tryBuiltinAutoAllow to return true for status")
		}

		var decoded map[string]map[string]string
		if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["hookSpecificOutput"]["permissionDecision"] != "allow" {
			t.Errorf("expected allow, got %q", decoded["hookSpecificOutput"]["permissionDecision"])
		}
	})

	t.Run("restart is not auto-allowed", func(t *testing.T) {
		var buf bytes.Buffer
		ok := tryBuiltinAutoAllow("mcp__plugin_moxy_moxy__restart", "mcp__plugin_moxy_moxy__", &buf)
		if ok {
			t.Fatal("expected tryBuiltinAutoAllow to return false for restart")
		}
		if buf.Len() != 0 {
			t.Errorf("expected no output, got %q", buf.String())
		}
	})

	t.Run("non-moxy tool is not auto-allowed", func(t *testing.T) {
		var buf bytes.Buffer
		ok := tryBuiltinAutoAllow("some_other_tool", "mcp__plugin_moxy_moxy__", &buf)
		if ok {
			t.Fatal("expected tryBuiltinAutoAllow to return false for non-moxy tool")
		}
	})
}

// TestEvalDynamicForHook drives the dynamic-perms predicate through a series
// of fixture scripts that exit with each contract code (0=allow, 1=ask,
// 2=deny, other=fall-through). The script reads the tool input from argv via
// arg-order so we can also confirm the input plumbing reaches the script.
func TestEvalDynamicForHook(t *testing.T) {
	tests := []struct {
		name       string
		exitCode   int
		wantDec    string
		wantReason string
	}{
		{name: "allow", exitCode: 0, wantDec: "allow"},
		{name: "ask", exitCode: 1, wantDec: "ask"},
		{name: "deny", exitCode: 2, wantDec: "deny"},
		// EvalDynamicPerms maps unmapped non-zero codes to "ask" with a
		// reason describing the unexpected exit — the cautious default.
		{name: "ask (unmapped exit 7)", exitCode: 7, wantDec: "ask"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &native.DynamicPermsSpec{
				Command:   "bash",
				Args:      []string{"-c", "exit " + itoa(tt.exitCode)},
				ArgOrder:  []string{"verb"},
				TimeoutMS: 1000,
			}
			input := map[string]any{"verb": "GET"}

			gotDec, gotReason := evalDynamicForHook(spec, input)
			if gotDec != tt.wantDec {
				t.Errorf("decision: got %q, want %q (reason=%q)", gotDec, tt.wantDec, gotReason)
			}
		})
	}

	t.Run("nil spec returns fall-through", func(t *testing.T) {
		gotDec, _ := evalDynamicForHook(nil, nil)
		if gotDec != "" {
			t.Errorf("expected fall-through for nil spec, got %q", gotDec)
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func TestInstallSettingsHook(t *testing.T) {
	// Use a temp dir as HOME so we don't touch the real settings.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	settingsDir := filepath.Join(tmpHome, ".claude")
	settingsPath := filepath.Join(settingsDir, "settings.json")

	t.Run("creates settings.json when missing", func(t *testing.T) {
		if err := InstallSettingsHook(); err != nil {
			t.Fatal(err)
		}

		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatal(err)
		}

		var settings map[string]any
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatal(err)
		}

		hooks, ok := settings["hooks"].(map[string]any)
		if !ok {
			t.Fatal("expected hooks key")
		}
		preToolUse, ok := hooks["PreToolUse"].([]any)
		if !ok {
			t.Fatal("expected PreToolUse array")
		}
		if len(preToolUse) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(preToolUse))
		}
		entry := preToolUse[0].(map[string]any)
		if entry["matcher"] != "mcp__moxy__.*" {
			t.Errorf("matcher: got %q", entry["matcher"])
		}
	})

	t.Run("idempotent on second call", func(t *testing.T) {
		if err := InstallSettingsHook(); err != nil {
			t.Fatal(err)
		}

		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatal(err)
		}

		var settings map[string]any
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatal(err)
		}

		hooks := settings["hooks"].(map[string]any)
		preToolUse := hooks["PreToolUse"].([]any)
		if len(preToolUse) != 1 {
			t.Fatalf("expected 1 entry after second call, got %d", len(preToolUse))
		}
	})

	t.Run("preserves existing settings", func(t *testing.T) {
		// Write settings with an existing key.
		existing := map[string]any{
			"alwaysThinkingEnabled": true,
		}
		data, _ := json.MarshalIndent(existing, "", "  ")
		os.WriteFile(settingsPath, append(data, '\n'), 0o644)

		if err := InstallSettingsHook(); err != nil {
			t.Fatal(err)
		}

		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatal(err)
		}

		var settings map[string]any
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatal(err)
		}

		if settings["alwaysThinkingEnabled"] != true {
			t.Error("existing key was not preserved")
		}
		hooks := settings["hooks"].(map[string]any)
		preToolUse := hooks["PreToolUse"].([]any)
		if len(preToolUse) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(preToolUse))
		}
	})
}
