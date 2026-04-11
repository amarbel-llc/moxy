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

// writeMoxinDir creates a directory-based moxin with _moxin.toml and tool files.
func writeMoxinDir(t *testing.T, parentDir, name string, moxinToml string, tools map[string]string) {
	t.Helper()
	dir := filepath.Join(parentDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_moxin.toml"), []byte(moxinToml), 0o644); err != nil {
		t.Fatal(err)
	}
	for toolName, toolContent := range tools {
		if err := os.WriteFile(filepath.Join(dir, toolName+".toml"), []byte(toolContent), 0o644); err != nil {
			t.Fatal(err)
		}
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
	writeMoxinDir(t, moxinDir, "test-server",
		"schema = 1\nname = \"test-server\"\ndescription = \"A test server\"\n",
		map[string]string{
			"hello": "schema = 1\ndescription = \"says hello\"\ncommand = \"echo\"\nargs = [\"hello\"]\n\n[input]\ntype = \"object\"\n",
		},
	)

	t.Setenv("MOXIN_PATH", moxinDir)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)
	output := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "test-server valid") {
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
	brokenDir := filepath.Join(moxinDir, "broken")
	os.MkdirAll(brokenDir, 0o755)
	os.WriteFile(filepath.Join(brokenDir, "_moxin.toml"), []byte("name = = broken"), 0o644)

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
	writeMoxinDir(t, moxinDir, "bad-server",
		"schema = 1\nname = \"bad-server\"\n",
		map[string]string{
			"hello": "schema = 1\n",
		},
	)

	t.Setenv("MOXIN_PATH", moxinDir)

	var buf bytes.Buffer
	code := Run(&buf, home, dir)
	output := buf.String()

	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "command is required") {
		t.Errorf("expected 'command is required' in output:\n%s", output)
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
	writeMoxinDir(t, moxinDir, "extra-server",
		"schema = 1\nname = \"extra-server\"\ndescription = \"has unknown keys\"\nbogus_field = \"oops\"\n",
		map[string]string{
			"hello": "schema = 1\ndescription = \"says hello\"\ncommand = \"echo\"\nargs = [\"hello\"]\nunknown_tool_key = true\n\n[input]\ntype = \"object\"\n",
		},
	)

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
