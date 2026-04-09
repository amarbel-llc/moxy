package native

import (
	"context"
	"encoding/json"
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
