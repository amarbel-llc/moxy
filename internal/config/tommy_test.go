package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amarbel-llc/tommy/pkg/marshal"
)

func TestTommyNoOpRoundTrip(t *testing.T) {
	input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"  # git operations\ncommand = \"grit mcp\"\n\n[[servers]]\nname = \"lux\"\ncommand = \"lux\"\n")

	var mf moxyfileConfig
	handle, err := marshal.UnmarshalDocument(input, &mf)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(mf.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(mf.Servers))
	}

	out, err := marshal.MarshalDocument(handle, &mf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != string(input) {
		t.Fatalf("no-op round-trip changed output.\nexpected:\n%s\ngot:\n%s", input, out)
	}
}

func TestTommyAppendPreservesComments(t *testing.T) {
	input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"  # git operations\ncommand = \"grit mcp\"\n")

	var mf moxyfileConfig
	handle, err := marshal.UnmarshalDocument(input, &mf)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	mf.Servers = append(mf.Servers, moxyfileServer{Name: "chix", Command: "chix mcp"})
	out, err := marshal.MarshalDocument(handle, &mf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
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

func TestTommyUpdateInPlacePreservesComments(t *testing.T) {
	input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"  # git operations\ncommand = \"grit mcp\"\n\n[[servers]]\nname = \"lux\"\ncommand = \"lux\"\n")

	var mf moxyfileConfig
	handle, err := marshal.UnmarshalDocument(input, &mf)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	mf.Servers[0].Command = "grit mcp --verbose"
	out, err := marshal.MarshalDocument(handle, &mf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
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

func TestToMoxyfileServer(t *testing.T) {
	srv := ServerConfig{
		Name:    "grit",
		Command: MakeCommand("grit", "mcp"),
	}
	mf := toMoxyfileServer(srv)
	if mf.Name != "grit" {
		t.Errorf("name: got %q", mf.Name)
	}
	if mf.Command != "grit mcp" {
		t.Errorf("command: got %q", mf.Command)
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
