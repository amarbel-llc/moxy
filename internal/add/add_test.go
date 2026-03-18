package add

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amarbel-llc/moxy/internal/config"
)

func TestFormatServerBlock(t *testing.T) {
	srv := config.ServerConfig{
		Name:    "grit",
		Command: config.MakeCommand("grit", "mcp"),
	}
	got := FormatServerBlock(srv)
	if !strings.Contains(got, `[[servers]]`) {
		t.Error("expected [[servers]] header")
	}
	if !strings.Contains(got, `name = "grit"`) {
		t.Error("expected name field")
	}
	if !strings.Contains(got, `command = "grit mcp"`) {
		t.Error("expected command field")
	}
}

func TestFormatServerBlockWithAnnotations(t *testing.T) {
	ro := true
	srv := config.ServerConfig{
		Name:    "grit",
		Command: config.MakeCommand("grit"),
		Annotations: &config.AnnotationFilter{
			ReadOnlyHint: &ro,
		},
	}
	got := FormatServerBlock(srv)
	if !strings.Contains(got, `readOnlyHint = true`) {
		t.Error("expected readOnlyHint annotation")
	}
}

func TestFormatServerBlockNoAnnotations(t *testing.T) {
	srv := config.ServerConfig{
		Name:    "lux",
		Command: config.MakeCommand("lux"),
	}
	got := FormatServerBlock(srv)
	if strings.Contains(got, "annotations") {
		t.Error("should not include annotations when none set")
	}
}

func TestAppendToFileCreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moxyfile")

	srv := config.ServerConfig{
		Name:    "grit",
		Command: config.MakeCommand("grit"),
	}
	if err := AppendServerToFile(path, srv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `name = "grit"`) {
		t.Errorf("expected grit in file, got:\n%s", data)
	}
}

func TestAppendToFileAppendsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moxyfile")

	existing := "[[servers]]\nname = \"grit\"\ncommand = \"grit\"\n"
	os.WriteFile(path, []byte(existing), 0o644)

	srv := config.ServerConfig{
		Name:    "lux",
		Command: config.MakeCommand("lux"),
	}
	if err := AppendServerToFile(path, srv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, `name = "grit"`) {
		t.Error("existing content should be preserved")
	}
	if !strings.Contains(content, `name = "lux"`) {
		t.Error("new server should be appended")
	}
}

func TestBuildServerConfigNoAnnotations(t *testing.T) {
	srv := buildServerConfig("grit", "grit mcp", nil)
	if srv.Name != "grit" {
		t.Errorf("expected name grit, got %q", srv.Name)
	}
	if srv.Command.Executable() != "grit" {
		t.Errorf("expected executable grit, got %q", srv.Command.Executable())
	}
	if srv.Annotations != nil {
		t.Error("expected nil annotations")
	}
}

func TestBuildServerConfigWithAnnotations(t *testing.T) {
	srv := buildServerConfig("grit", "grit", []string{"readOnlyHint", "destructiveHint"})
	if srv.Annotations == nil {
		t.Fatal("expected annotations")
	}
	if srv.Annotations.ReadOnlyHint == nil || !*srv.Annotations.ReadOnlyHint {
		t.Error("expected readOnlyHint = true")
	}
	if srv.Annotations.DestructiveHint == nil || !*srv.Annotations.DestructiveHint {
		t.Error("expected destructiveHint = true")
	}
	if srv.Annotations.IdempotentHint != nil {
		t.Error("expected idempotentHint to be nil")
	}
}
