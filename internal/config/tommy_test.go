package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodegenNoOpRoundTrip(t *testing.T) {
	input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"\ncommand = \"grit mcp\"\n\n[[servers]]\nname = \"lux\"\ncommand = \"lux\"\n")

	doc, err := DecodeConfig(input)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(doc.Data().Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(doc.Data().Servers))
	}

	out, err := doc.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(out) != string(input) {
		t.Fatalf("no-op round-trip changed output.\nexpected:\n%s\ngot:\n%s", input, out)
	}
}

func TestCodegenAppendPreservesComments(t *testing.T) {
	input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"  # git operations\ncommand = \"grit mcp\"\n")

	doc, err := DecodeConfig(input)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	cfg := doc.Data()
	cfg.Servers = append(cfg.Servers, ServerConfig{
		Name:    "chix",
		Command: MakeCommand("chix", "mcp"),
	})

	out, err := doc.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	outStr := string(out)
	if !strings.HasPrefix(outStr, "# my MCP servers\n") {
		t.Error("top comment lost after append")
	}
	if !strings.Contains(outStr, "# git operations") {
		t.Error("inline comment lost after append")
	}
	if !strings.Contains(outStr, `name = "chix"`) {
		t.Error("appended server not found")
	}
}

func TestCodegenUpdateInPlacePreservesComments(t *testing.T) {
	input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"  # git operations\ncommand = \"grit mcp\"\n\n[[servers]]\nname = \"lux\"\ncommand = \"lux\"\n")

	doc, err := DecodeConfig(input)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	cfg := doc.Data()
	cfg.Servers[0].Command = MakeCommand("grit", "mcp", "--verbose")

	out, err := doc.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	outStr := string(out)
	if !strings.Contains(outStr, `command = "grit mcp --verbose"`) {
		t.Error("grit command not updated in place")
	}
	if !strings.Contains(outStr, "# git operations") {
		t.Error("inline comment lost after update")
	}
	if !strings.Contains(outStr, "# my MCP servers") {
		t.Error("top comment lost after update")
	}
}

func TestWriteServerCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moxyfile")

	srv := ServerConfig{
		Name:    "grit",
		Command: MakeCommand("grit", "mcp"),
	}
	if err := WriteServer(path, srv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, `name = "grit"`) {
		t.Errorf("expected grit in file, got:\n%s", content)
	}
	if !strings.Contains(content, `command = "grit mcp"`) {
		t.Errorf("expected command in file, got:\n%s", content)
	}
}

func TestWriteServerAppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moxyfile")
	os.WriteFile(path, []byte("# config\n\n[[servers]]\nname = \"grit\"\ncommand = \"grit\"\n"), 0o644)

	srv := ServerConfig{
		Name:    "lux",
		Command: MakeCommand("lux"),
	}
	if err := WriteServer(path, srv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "# config") {
		t.Error("comment lost")
	}
	if !strings.Contains(content, `name = "grit"`) {
		t.Error("existing server lost")
	}
	if !strings.Contains(content, `name = "lux"`) {
		t.Error("new server not appended")
	}
}

func TestWriteServerUpdatesExistingByName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moxyfile")
	os.WriteFile(path, []byte("# config\n\n[[servers]]\nname = \"grit\"  # git\ncommand = \"grit\"\n"), 0o644)

	srv := ServerConfig{
		Name:    "grit",
		Command: MakeCommand("grit", "mcp", "--verbose"),
	}
	if err := WriteServer(path, srv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "# config") {
		t.Error("top comment lost")
	}
	if !strings.Contains(content, "# git") {
		t.Error("inline comment lost")
	}
	if !strings.Contains(content, `command = "grit mcp --verbose"`) {
		t.Error("command not updated")
	}
	if strings.Count(content, `name = "grit"`) != 1 {
		t.Error("duplicate grit entry")
	}
}
