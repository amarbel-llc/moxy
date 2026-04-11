package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseNativeToolName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantOK   bool
	}{
		{
			name:   "simple tool",
			input:  "mcp__moxy__folio_read",
			want:   "folio.read",
			wantOK: true,
		},
		{
			name:   "hyphenated server",
			input:  "mcp__moxy__folio-external_read",
			want:   "folio-external.read",
			wantOK: true,
		},
		{
			name:   "hyphenated tool",
			input:  "mcp__moxy__just-us-agents_list-recipes",
			want:   "just-us-agents.list-recipes",
			wantOK: true,
		},
		{
			name:   "tool with hyphen",
			input:  "mcp__moxy__gordo_mod-read",
			want:   "gordo.mod-read",
			wantOK: true,
		},
		{
			name:   "builtin tool",
			input:  "Bash",
			want:   "",
			wantOK: false,
		},
		{
			name:   "no tool part",
			input:  "mcp__moxy__restart",
			want:   "",
			wantOK: false,
		},
		{
			name:   "different mcp server",
			input:  "mcp__other__foo_bar",
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseNativeToolName(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok: got %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("result: got %q, want %q", got, tt.want)
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
		if entry["matcher"] != moxyToolPrefix+".*" {
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
