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
