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

	writeFile(t, filepath.Join(dir, "moxyfile"), `name = = broken`)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)

	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
}

func TestRunUnknownFieldsIgnored(t *testing.T) {
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

	if code != 0 {
		t.Fatalf("expected exit 0 (unknown fields ignored), got %d\noutput:\n%s", code, output)
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

func TestRunMoxinValid(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	os.MkdirAll(dir, 0o755)

	// Need at least one moxyfile server to avoid "no servers configured"
	writeFile(t, filepath.Join(dir, "moxyfile"), `
[[servers]]
name = "grit"
command = "grit"
`)

	moxinDir := filepath.Join(t.TempDir(), "moxins")
	writeFile(t, filepath.Join(moxinDir, "test.toml"), `
name = "test-server"
description = "A test server"

[[tools]]
name = "hello"
description = "says hello"
command = "echo"
args = ["hello"]

[tools.input]
type = "object"
`)

	t.Setenv("MOXIN_PATH", moxinDir)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)
	output := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "test.toml valid") {
		t.Errorf("expected moxin config validation in output:\n%s", output)
	}
	if !strings.Contains(output, "moxin: 1 server") {
		t.Errorf("expected moxin server count in output:\n%s", output)
	}
}

func TestRunMoxinInvalidToml(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	os.MkdirAll(dir, 0o755)

	writeFile(t, filepath.Join(dir, "moxyfile"), `
[[servers]]
name = "grit"
command = "grit"
`)

	moxinDir := filepath.Join(t.TempDir(), "moxins")
	writeFile(t, filepath.Join(moxinDir, "broken.toml"), `name = = broken`)

	t.Setenv("MOXIN_PATH", moxinDir)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)
	output := buf.String()

	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "not ok") {
		t.Errorf("expected not-ok for broken moxin config:\n%s", output)
	}
}

func TestRunMoxinMissingCommand(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	os.MkdirAll(dir, 0o755)

	writeFile(t, filepath.Join(dir, "moxyfile"), `
[[servers]]
name = "grit"
command = "grit"
`)

	moxinDir := filepath.Join(t.TempDir(), "moxins")
	writeFile(t, filepath.Join(moxinDir, "bad.toml"), `
name = "bad-server"

[[tools]]
name = "hello"
`)

	t.Setenv("MOXIN_PATH", moxinDir)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)
	output := buf.String()

	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "missing command") {
		t.Errorf("expected 'missing command' in output:\n%s", output)
	}
}

func TestRunMoxinUndecodedKeys(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	os.MkdirAll(dir, 0o755)

	writeFile(t, filepath.Join(dir, "moxyfile"), `
[[servers]]
name = "grit"
command = "grit"
`)

	moxinDir := filepath.Join(t.TempDir(), "moxins")
	writeFile(t, filepath.Join(moxinDir, "extra.toml"), `
name = "extra-server"
description = "has unknown keys"
bogus_field = "oops"

[[tools]]
name = "hello"
description = "says hello"
command = "echo"
args = ["hello"]
unknown_tool_key = true

[tools.input]
type = "object"
`)

	t.Setenv("MOXIN_PATH", moxinDir)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)
	output := buf.String()

	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "undecoded keys") {
		t.Errorf("expected 'undecoded keys' in output:\n%s", output)
	}
	if !strings.Contains(output, "bogus_field") {
		t.Errorf("expected 'bogus_field' in undecoded keys:\n%s", output)
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
