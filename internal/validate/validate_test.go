package validate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestRunValidConfig(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	os.MkdirAll(dir, 0o755)

	writeFile(t, filepath.Join(dir, "moxyfile"), `
[[servers]]
name = "grit"
command = "grit mcp"
`)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)
	output := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "merged: 2 server") == false && !strings.Contains(output, "merged: 1 server") {
		t.Errorf("expected merged server count in output:\n%s", output)
	}
	if !strings.Contains(output, "all servers valid") {
		t.Errorf("expected 'all servers valid' in output:\n%s", output)
	}
}

func TestRunNoServers(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	os.MkdirAll(dir, 0o755)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)
	output := buf.String()

	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "no servers configured") {
		t.Errorf("expected 'no servers configured' in output:\n%s", output)
	}
}

func TestRunInvalidToml(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	os.MkdirAll(dir, 0o755)

	writeFile(t, filepath.Join(dir, "moxyfile"), `this is not valid toml [[[`)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)

	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
}

func TestRunUnknownFields(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	os.MkdirAll(dir, 0o755)

	writeFile(t, filepath.Join(dir, "moxyfile"), `
[[servers]]
name = "grit"
command = "grit"
bogus_field = "oops"
`)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)
	output := buf.String()

	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "unknown fields") {
		t.Errorf("expected 'unknown fields' in output:\n%s", output)
	}
}

func TestRunMissingCommand(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	os.MkdirAll(dir, 0o755)

	writeFile(t, filepath.Join(dir, "moxyfile"), `
[[servers]]
name = "grit"
`)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)
	output := buf.String()

	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "no command") {
		t.Errorf("expected 'no command' in output:\n%s", output)
	}
}

func TestRunSkipsNotFound(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	os.MkdirAll(dir, 0o755)

	writeFile(t, filepath.Join(home, ".config", "moxy", "moxyfile"), `
[[servers]]
name = "grit"
command = "grit"
`)

	var buf bytes.Buffer
	Run(&buf, home, dir)
	output := buf.String()

	if !strings.Contains(output, "SKIP") {
		t.Errorf("expected SKIP for missing repo moxyfile:\n%s", output)
	}
	if !strings.Contains(output, "valid") {
		t.Errorf("expected valid for global moxyfile:\n%s", output)
	}
}

func TestRunHierarchyMerge(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	os.MkdirAll(dir, 0o755)

	writeFile(t, filepath.Join(home, ".config", "moxy", "moxyfile"), `
[[servers]]
name = "grit"
command = "grit"
`)
	writeFile(t, filepath.Join(dir, "moxyfile"), `
[[servers]]
name = "lux"
command = "lux"
`)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)
	output := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "2 server") {
		t.Errorf("expected '2 server' in output:\n%s", output)
	}
}
