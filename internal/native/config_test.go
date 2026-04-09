package native

import (
	"encoding/json"
	"testing"
)

func TestParseConfig(t *testing.T) {
	data := []byte(`
name = "shell"
description = "Shell execution"

[[tools]]
name = "exec"
description = "Execute a shell command"
command = "sh"
args = ["-c"]

[tools.input.properties.command]
type = "string"
description = "Shell command to execute"

[tools.input]
required = ["command"]
`)

	cfg, err := ParseConfig(data)
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

	// Verify the Input field was converted to valid JSON containing the schema.
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

func TestParseConfigRequiredOrder(t *testing.T) {
	data := []byte(`
name = "folio"

[[tools]]
name = "read"
command = "sh"
args = ["-c", "echo $@", "_"]

[tools.input]
type = "object"
required = ["file_path"]

[tools.input.properties.file_path]
type = "string"

[[tools]]
name = "read_range"
command = "sh"
args = ["-c", "echo $@", "_"]

[tools.input]
type = "object"
required = ["file_path", "start", "end"]

[tools.input.properties.file_path]
type = "string"

[tools.input.properties.start]
type = "integer"

[tools.input.properties.end]
type = "integer"
`)

	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Tools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(cfg.Tools))
	}

	// First tool: read (single required arg)
	t.Logf("read Input JSON: %s", string(cfg.Tools[0].Input))
	readArgs, err := buildExtraArgs(
		json.RawMessage(`{"file_path":"/tmp/x.go"}`),
		cfg.Tools[0].Input,
		cfg.Tools[0].ArgOrder,
	)
	if err != nil {
		t.Fatalf("buildExtraArgs (read) error: %v", err)
	}
	if len(readArgs) != 1 || readArgs[0] != "/tmp/x.go" {
		t.Errorf("read args = %v, want [\"/tmp/x.go\"]", readArgs)
	}

	// Second tool: read_range (three required args in order)
	t.Logf("read_range Input JSON: %s", string(cfg.Tools[1].Input))
	rangeArgs, err := buildExtraArgs(
		json.RawMessage(`{"file_path":"/tmp/x.go","start":1,"end":5}`),
		cfg.Tools[1].Input,
		cfg.Tools[1].ArgOrder,
	)
	if err != nil {
		t.Fatalf("buildExtraArgs (read_range) error: %v", err)
	}
	t.Logf("read_range args: %v", rangeArgs)

	if len(rangeArgs) != 3 {
		t.Fatalf("len(args) = %d, want 3", len(rangeArgs))
	}
	if rangeArgs[0] != "/tmp/x.go" {
		t.Errorf("args[0] = %q, want \"/tmp/x.go\"", rangeArgs[0])
	}
	if rangeArgs[1] != "1" {
		t.Errorf("args[1] = %q, want \"1\"", rangeArgs[1])
	}
	if rangeArgs[2] != "5" {
		t.Errorf("args[2] = %q, want \"5\"", rangeArgs[2])
	}
}

func TestParseConfigArgOrder(t *testing.T) {
	data := []byte(`
name = "rg"

[[tools]]
name = "search"
command = "sh"
args = ["-c", """
set -euo pipefail
pattern="$1"; shift
rg "$pattern" "$@"
""", "_"]
arg_order = ["pattern", "path", "type", "glob"]

[tools.input]
type = "object"
required = ["pattern"]

[tools.input.properties.pattern]
type = "string"

[tools.input.properties.path]
type = "string"

[tools.input.properties.type]
type = "string"

[tools.input.properties.glob]
type = "string"
`)

	cfg, err := ParseConfig(data)
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

	// All args provided — order matches arg_order
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

	// Only required arg — trailing optionals trimmed
	reqOnly, err := buildExtraArgs(
		json.RawMessage(`{"pattern":"TODO"}`),
		tool.Input,
		tool.ArgOrder,
	)
	if err != nil {
		t.Fatalf("buildExtraArgs error: %v", err)
	}
	if len(reqOnly) != 1 || reqOnly[0] != "TODO" {
		t.Errorf("required-only args = %v, want [TODO]", reqOnly)
	}

	// Middle arg absent — empty string preserves positions
	midAbsent, err := buildExtraArgs(
		json.RawMessage(`{"pattern":"TODO","glob":"*.go"}`),
		tool.Input,
		tool.ArgOrder,
	)
	if err != nil {
		t.Fatalf("buildExtraArgs error: %v", err)
	}
	// arg_order is [pattern, path, type, glob] — path and type absent but glob present
	if len(midAbsent) != 4 || midAbsent[0] != "TODO" || midAbsent[1] != "" || midAbsent[2] != "" || midAbsent[3] != "*.go" {
		t.Errorf("mid-absent args = %v, want [TODO \"\" \"\" *.go]", midAbsent)
	}
}

func TestParseConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "missing server name",
			data: `description = "no name"`,
		},
		{
			name: "dots in server name",
			data: `name = "my.server"`,
		},
		{
			name: "tool missing name",
			data: `
name = "shell"
[[tools]]
command = "echo"
`,
		},
		{
			name: "tool missing command",
			data: `
name = "shell"
[[tools]]
name = "exec"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig([]byte(tt.data))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}
