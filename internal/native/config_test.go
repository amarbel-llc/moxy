package native

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeMoxinDir creates a moxin directory with _moxin.toml and tool files.
func writeMoxinDir(t *testing.T, base, name string, meta string, tools map[string]string) string {
	t.Helper()
	dir := filepath.Join(base, name)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "_moxin.toml"), []byte(meta), 0o644)
	for toolName, content := range tools {
		os.WriteFile(filepath.Join(dir, toolName+".toml"), []byte(content), 0o644)
	}
	return dir
}

func TestParseMoxinDir(t *testing.T) {
	dir := writeMoxinDir(t, t.TempDir(), "shell", `
schema = 1
name = "shell"
description = "Shell execution"
`, map[string]string{
		"exec": `
schema = 1
description = "Execute a shell command"
command = "sh"
args = ["-c"]

[input]
required = ["command"]

[input.properties.command]
type = "string"
description = "Shell command to execute"
`,
	})

	cfg, err := ParseMoxinDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Name != "shell" {
		t.Errorf("name = %q, want %q", cfg.Name, "shell")
	}
	if cfg.Description != "Shell execution" {
		t.Errorf("description = %q, want %q", cfg.Description, "Shell execution")
	}
	if len(cfg.Tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1", len(cfg.Tools))
	}

	tool := cfg.Tools[0]
	if tool.Name != "exec" {
		t.Errorf("tool.name = %q, want %q", tool.Name, "exec")
	}
	if tool.Description != "Execute a shell command" {
		t.Errorf("tool.description = %q, want %q", tool.Description, "Execute a shell command")
	}
	if tool.Command != "sh" {
		t.Errorf("tool.command = %q, want %q", tool.Command, "sh")
	}
	if len(tool.Args) != 1 || tool.Args[0] != "-c" {
		t.Errorf("tool.args = %v, want [\"-c\"]", tool.Args)
	}

	// Verify the Input field was converted to valid JSON.
	if tool.Input == nil {
		t.Fatal("tool.input is nil, expected JSON schema")
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.Input, &schema); err != nil {
		t.Fatalf("tool.input is not valid JSON: %v", err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("input.properties missing or wrong type: %v", schema)
	}
	cmdProp, ok := props["command"].(map[string]any)
	if !ok {
		t.Fatal("input.properties.command missing")
	}
	if cmdProp["type"] != "string" {
		t.Errorf("input.properties.command.type = %v, want \"string\"", cmdProp["type"])
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("input.required missing or wrong type: %v", schema)
	}
	if len(required) != 1 || required[0] != "command" {
		t.Errorf("input.required = %v, want [\"command\"]", required)
	}
}

func TestParseMoxinDirToolNameFromFile(t *testing.T) {
	dir := writeMoxinDir(t, t.TempDir(), "test", `
schema = 1
name = "test"
`, map[string]string{
		"my-tool": `
schema = 1
command = "echo"
`,
	})

	cfg, err := ParseMoxinDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tools[0].Name != "my-tool" {
		t.Errorf("tool name = %q, want %q (from filename)", cfg.Tools[0].Name, "my-tool")
	}
}

func TestParseMoxinDirToolNameOverride(t *testing.T) {
	dir := writeMoxinDir(t, t.TempDir(), "test", `
schema = 1
name = "test"
`, map[string]string{
		"filename": `
schema = 1
name = "override"
command = "echo"
`,
	})

	cfg, err := ParseMoxinDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Tools[0].Name != "override" {
		t.Errorf("tool name = %q, want %q (from name field)", cfg.Tools[0].Name, "override")
	}
}

func TestParseMoxinDirAlphabeticalOrder(t *testing.T) {
	dir := writeMoxinDir(t, t.TempDir(), "test", `
schema = 1
name = "test"
`, map[string]string{
		"zebra":   "schema = 1\ncommand = \"echo\"",
		"alpha":   "schema = 1\ncommand = \"echo\"",
		"middle":  "schema = 1\ncommand = \"echo\"",
	})

	cfg, err := ParseMoxinDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Tools) != 3 {
		t.Fatalf("len(tools) = %d, want 3", len(cfg.Tools))
	}
	want := []string{"alpha", "middle", "zebra"}
	for i, w := range want {
		if cfg.Tools[i].Name != w {
			t.Errorf("tools[%d].name = %q, want %q", i, cfg.Tools[i].Name, w)
		}
	}
}

func TestParseMoxinDirPermsRequest(t *testing.T) {
	dir := writeMoxinDir(t, t.TempDir(), "test", `
schema = 1
name = "test"
`, map[string]string{
		"allowed": `
schema = 1
command = "echo"
perms-request = "always-allow"
`,
		"each": `
schema = 1
command = "echo"
perms-request = "each-use"
`,
		"delegated": `
schema = 1
command = "echo"
perms-request = "delegate-to-client"
`,
		"default": `
schema = 1
command = "echo"
`,
	})

	cfg, err := ParseMoxinDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	perms := make(map[string]PermsRequest)
	for _, tool := range cfg.Tools {
		perms[tool.Name] = tool.PermsRequest
	}

	if perms["allowed"] != PermsAlwaysAllow {
		t.Errorf("allowed perms = %q, want %q", perms["allowed"], PermsAlwaysAllow)
	}
	if perms["each"] != PermsEachUse {
		t.Errorf("each perms = %q, want %q", perms["each"], PermsEachUse)
	}
	if perms["delegated"] != PermsDelegateToClient {
		t.Errorf("delegated perms = %q, want %q", perms["delegated"], PermsDelegateToClient)
	}
	if perms["default"] != "" {
		t.Errorf("default perms = %q, want empty", perms["default"])
	}
}

func TestParseMoxinDirInvalidPermsRequest(t *testing.T) {
	dir := writeMoxinDir(t, t.TempDir(), "test", `
schema = 1
name = "test"
`, map[string]string{
		"bad": `
schema = 1
command = "echo"
perms-request = "bogus"
`,
	})

	_, err := ParseMoxinDir(dir)
	if err == nil {
		t.Fatal("expected error for invalid perms-request, got nil")
	}
}

func TestParseMoxinDirArgOrder(t *testing.T) {
	dir := writeMoxinDir(t, t.TempDir(), "rg", `
schema = 1
name = "rg"
`, map[string]string{
		"search": `
schema = 1
command = "sh"
args = ["-c", "rg \"$@\"", "_"]
arg_order = ["pattern", "path", "type", "glob"]

[input]
type = "object"
required = ["pattern"]

[input.properties.pattern]
type = "string"

[input.properties.path]
type = "string"

[input.properties.type]
type = "string"

[input.properties.glob]
type = "string"
`,
	})

	cfg, err := ParseMoxinDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tool := cfg.Tools[0]
	if len(tool.ArgOrder) != 4 {
		t.Fatalf("len(arg_order) = %d, want 4", len(tool.ArgOrder))
	}
	want := []string{"pattern", "path", "type", "glob"}
	for i, w := range want {
		if tool.ArgOrder[i] != w {
			t.Errorf("arg_order[%d] = %q, want %q", i, tool.ArgOrder[i], w)
		}
	}

	// Verify buildExtraArgs still works with the parsed tool.
	allArgs, err := buildExtraArgs(
		json.RawMessage(`{"pattern":"TODO","path":"/src","type":"go","glob":"*.go"}`),
		tool.Input,
		tool.ArgOrder,
	)
	if err != nil {
		t.Fatalf("buildExtraArgs error: %v", err)
	}
	if len(allArgs) != 4 || allArgs[0] != "TODO" || allArgs[1] != "/src" || allArgs[2] != "go" || allArgs[3] != "*.go" {
		t.Errorf("all args = %v, want [TODO /src go *.go]", allArgs)
	}
}

func TestParseMoxinDirValidation(t *testing.T) {
	tests := []struct {
		name  string
		meta  string
		tools map[string]string
	}{
		{
			name:  "missing schema in meta",
			meta:  `name = "test"`,
			tools: map[string]string{"t": "schema = 1\ncommand = \"echo\""},
		},
		{
			name:  "wrong schema in meta",
			meta:  "schema = 2\nname = \"test\"",
			tools: map[string]string{"t": "schema = 1\ncommand = \"echo\""},
		},
		{
			name:  "missing name in meta",
			meta:  "schema = 1",
			tools: map[string]string{"t": "schema = 1\ncommand = \"echo\""},
		},
		{
			name:  "dots in name",
			meta:  "schema = 1\nname = \"my.server\"",
			tools: map[string]string{"t": "schema = 1\ncommand = \"echo\""},
		},
		{
			name:  "missing schema in tool",
			meta:  "schema = 1\nname = \"test\"",
			tools: map[string]string{"t": "command = \"echo\""},
		},
		{
			name:  "missing command in tool",
			meta:  "schema = 1\nname = \"test\"",
			tools: map[string]string{"t": "schema = 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeMoxinDir(t, t.TempDir(), "test", tt.meta, tt.tools)
			_, err := ParseMoxinDir(dir)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestParseMoxinDirUndecoded(t *testing.T) {
	dir := writeMoxinDir(t, t.TempDir(), "test", `
schema = 1
name = "test"
bogus = "oops"
`, map[string]string{
		"tool": `
schema = 1
command = "echo"
unknown_key = true
`,
	})

	result, err := ParseMoxinDirFull(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Undecoded) == 0 {
		t.Fatal("expected undecoded keys, got none")
	}
}
