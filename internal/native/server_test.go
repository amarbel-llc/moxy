package native

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

func TestServerName(t *testing.T) {
	cfg := &NativeConfig{Name: "test-server"}
	srv := NewServer(cfg)
	if got := srv.Name(); got != "test-server" {
		t.Fatalf("Name() = %q, want %q", got, "test-server")
	}
}

func TestServerToolsList(t *testing.T) {
	cfg := &NativeConfig{
		Name: "test-server",
		Tools: []ToolSpec{
			{
				Name:        "greet",
				Description: "Says hello",
				Command:     "echo",
				Args:        []string{"hello"},
			},
		},
	}
	srv := NewServer(cfg)

	raw, err := srv.Call(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatalf("Call tools/list: %v", err)
	}

	var result protocol.ToolsListResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "greet" {
		t.Errorf("tool name = %q, want %q", result.Tools[0].Name, "greet")
	}
	if result.Tools[0].Description != "Says hello" {
		t.Errorf("tool description = %q, want %q", result.Tools[0].Description, "Says hello")
	}
}

func TestServerToolsCall(t *testing.T) {
	cfg := &NativeConfig{
		Name: "test-server",
		Tools: []ToolSpec{
			{
				Name:    "echo-hello",
				Command: "echo",
				Args:    []string{"-n", "hello world"},
			},
		},
	}
	srv := NewServer(cfg)

	params := protocol.ToolCallParams{
		Name: "echo-hello",
	}
	raw, err := srv.Call(context.Background(), "tools/call", params)
	if err != nil {
		t.Fatalf("Call tools/call: %v", err)
	}

	var result protocol.ToolCallResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.IsError {
		t.Fatal("expected IsError=false")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Text != "hello world" {
		t.Errorf("output = %q, want %q", result.Content[0].Text, "hello world")
	}
}

func TestServerToolsCallUnknown(t *testing.T) {
	cfg := &NativeConfig{
		Name: "test-server",
		Tools: []ToolSpec{
			{
				Name:    "exists",
				Command: "echo",
			},
		},
	}
	srv := NewServer(cfg)

	params := protocol.ToolCallParams{
		Name: "does-not-exist",
	}
	raw, err := srv.Call(context.Background(), "tools/call", params)
	if err != nil {
		t.Fatalf("Call tools/call: %v", err)
	}

	var result protocol.ToolCallResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if !result.IsError {
		t.Fatal("expected IsError=true for unknown tool")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected error content")
	}
	if result.Content[0].Text == "" {
		t.Error("expected non-empty error message")
	}
}

func TestServerToolsCallWithArguments(t *testing.T) {
	cfg := &NativeConfig{
		Name: "test-server",
		Tools: []ToolSpec{
			{
				Name:    "exec",
				Command: "sh",
				Args:    []string{"-c"},
				Input:   json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
			},
		},
	}
	srv := NewServer(cfg)

	params := protocol.ToolCallParams{
		Name:      "exec",
		Arguments: json.RawMessage(`{"command":"echo -n hello from args"}`),
	}
	raw, err := srv.Call(context.Background(), "tools/call", params)
	if err != nil {
		t.Fatalf("Call tools/call: %v", err)
	}

	var result protocol.ToolCallResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected IsError=false, got error: %s", result.Content[0].Text)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Text != "hello from args" {
		t.Errorf("output = %q, want %q", result.Content[0].Text, "hello from args")
	}
}

func TestServerToolsCallCachesLargeOutput(t *testing.T) {
	cacheDir := t.TempDir()
	cfg := &NativeConfig{
		Name: "test-server",
		Tools: []ToolSpec{
			{
				Name:    "exec",
				Command: "sh",
				Args:    []string{"-c"},
				Input:   json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
			},
		},
	}
	srv := NewServer(cfg)
	srv.cache = newResultCache(cacheDir)
	srv.SetSession("test-session")

	params := protocol.ToolCallParams{
		Name:      "exec",
		Arguments: json.RawMessage(`{"command":"seq 1 100"}`),
	}
	raw, err := srv.Call(context.Background(), "tools/call", params)
	if err != nil {
		t.Fatalf("Call tools/call: %v", err)
	}

	var result protocol.ToolCallResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected IsError=false, got error: %s", result.Content[0].Text)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	text := result.Content[0].Text
	if !strings.Contains(text, "moxy.native://results/test-session/") {
		t.Errorf("expected cached result URI in output, got:\n%s", text)
	}
	if !strings.Contains(text, "First 10 lines") {
		t.Errorf("expected head section in summary, got:\n%s", text)
	}
	if !strings.Contains(text, "Last 10 lines") {
		t.Errorf("expected tail section in summary, got:\n%s", text)
	}
}

func TestServerToolsCallURISubstitution(t *testing.T) {
	cacheDir := t.TempDir()
	cfg := &NativeConfig{
		Name: "test-server",
		Tools: []ToolSpec{
			{
				Name:    "exec",
				Command: "sh",
				Args:    []string{"-c"},
				Input:   json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
			},
		},
	}
	srv := NewServer(cfg)
	srv.cache = newResultCache(cacheDir)
	srv.SetSession("test-session")

	// Pre-populate the cache with known content.
	if err := srv.cache.store(cachedResult{
		ID:      "test-abc",
		Session: "test-session",
		Output:  "1\n2\n3\n4\n5\n",
	}); err != nil {
		t.Fatalf("store: %v", err)
	}

	// Use the cached URI in a grep command.
	params := protocol.ToolCallParams{
		Name:      "exec",
		Arguments: json.RawMessage(`{"command":"grep -x 3 moxy.native://results/test-session/test-abc"}`),
	}
	raw, err := srv.Call(context.Background(), "tools/call", params)
	if err != nil {
		t.Fatalf("Call tools/call: %v", err)
	}

	var result protocol.ToolCallResultV1
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected IsError=false, got error: %s", result.Content[0].Text)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if strings.TrimSpace(result.Content[0].Text) != "3" {
		t.Errorf("output = %q, want %q", result.Content[0].Text, "3\n")
	}
}

func TestBuildExtraArgs(t *testing.T) {
	t.Run("nil arguments", func(t *testing.T) {
		args, err := buildExtraArgs(nil, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 0 {
			t.Errorf("expected empty args, got %v", args)
		}
	})

	t.Run("single string argument", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"command":"echo hello"}`),
			json.RawMessage(`{"required":["command"]}`),
			nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 1 || args[0] != "echo hello" {
			t.Errorf("args = %v, want [\"echo hello\"]", args)
		}
	})

	t.Run("ordering follows required array", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"b":"second","a":"first"}`),
			json.RawMessage(`{"required":["a","b"]}`),
			nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 2 || args[0] != "first" || args[1] != "second" {
			t.Errorf("args = %v, want [\"first\", \"second\"]", args)
		}
	})

	t.Run("unrequired keys sorted alphabetically", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"z":"last","a":"first","m":"middle"}`),
			nil,
			nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 3 || args[0] != "first" || args[1] != "middle" || args[2] != "last" {
			t.Errorf("args = %v, want [\"first\", \"middle\", \"last\"]", args)
		}
	})

	t.Run("non-string values use raw representation", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"count":42}`),
			nil,
			nil,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 1 || args[0] != "42" {
			t.Errorf("args = %v, want [\"42\"]", args)
		}
	})

	t.Run("arg_order takes precedence over required", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"pattern":"TODO","path":"/src","type":"go"}`),
			json.RawMessage(`{"required":["pattern"]}`),
			[]string{"pattern", "path", "type"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 3 || args[0] != "TODO" || args[1] != "/src" || args[2] != "go" {
			t.Errorf("args = %v, want [\"TODO\", \"/src\", \"go\"]", args)
		}
	})

	t.Run("arg_order emits empty for absent middle args", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"pattern":"TODO","type":"go"}`),
			json.RawMessage(`{"required":["pattern"]}`),
			[]string{"pattern", "path", "type"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// path is absent but type is present, so path gets "" to keep positions stable
		if len(args) != 3 || args[0] != "TODO" || args[1] != "" || args[2] != "go" {
			t.Errorf("args = %v, want [\"TODO\", \"\", \"go\"]", args)
		}
	})

	t.Run("arg_order trims trailing empty slots", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"pattern":"TODO"}`),
			json.RawMessage(`{"required":["pattern"]}`),
			[]string{"pattern", "path", "type"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// path and type absent at the tail — trimmed
		if len(args) != 1 || args[0] != "TODO" {
			t.Errorf("args = %v, want [\"TODO\"]", args)
		}
	})

	t.Run("arg_order with extra unlisted keys appended sorted", func(t *testing.T) {
		args, err := buildExtraArgs(
			json.RawMessage(`{"pattern":"TODO","path":"/src","extra_b":"B","extra_a":"A"}`),
			nil,
			[]string{"pattern", "path"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(args) != 4 || args[0] != "TODO" || args[1] != "/src" || args[2] != "A" || args[3] != "B" {
			t.Errorf("args = %v, want [\"TODO\", \"/src\", \"A\", \"B\"]", args)
		}
	})
}
