package native

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDiscoveryMoxin creates a directory-based moxin with _moxin.toml and a default tool.
func writeDiscoveryMoxin(t *testing.T, parentDir, name, desc string) {
	t.Helper()
	dir := filepath.Join(parentDir, name)
	os.MkdirAll(dir, 0o755)
	meta := fmt.Sprintf("schema = 1\nname = %q\ndescription = %q\n", name, desc)
	os.WriteFile(filepath.Join(dir, "_moxin.toml"), []byte(meta), 0o644)
	os.WriteFile(filepath.Join(dir, "hello.toml"), []byte("schema = 1\ncommand = \"echo\"\n"), 0o644)
}

func TestDiscoverConfigsFromMoxinPath(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")

	writeDiscoveryMoxin(t, dirA, "alpha", "from A")
	writeDiscoveryMoxin(t, dirB, "beta", "from B")

	moxinPath := dirA + ":" + dirB

	configs, err := DiscoverConfigs(moxinPath, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 2 {
		t.Fatalf("len(configs) = %d, want 2", len(configs))
	}

	names := map[string]bool{}
	for _, cfg := range configs {
		names[cfg.Name] = true
	}
	if !names["alpha"] {
		t.Error("expected alpha in discovered configs")
	}
	if !names["beta"] {
		t.Error("expected beta in discovered configs")
	}
}

func TestMoxinPathEarlierOverridesLater(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")

	writeDiscoveryMoxin(t, dirA, "tool", "from-A")
	writeDiscoveryMoxin(t, dirB, "tool", "from-B")

	// A is earlier in path → A should win
	moxinPath := dirA + ":" + dirB

	configs, err := DiscoverConfigs(moxinPath, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	if configs[0].Description != "from-A" {
		t.Errorf("expected from-A, got description=%q", configs[0].Description)
	}
}

func TestSystemDirAppended(t *testing.T) {
	userDir := filepath.Join(t.TempDir(), "user")
	systemDir := filepath.Join(t.TempDir(), "system")

	writeDiscoveryMoxin(t, userDir, "tool", "user-version")
	writeDiscoveryMoxin(t, systemDir, "tool", "system-version")
	writeDiscoveryMoxin(t, systemDir, "builtin", "system-only")

	configs, err := DiscoverConfigs(userDir, systemDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 2 {
		t.Fatalf("len(configs) = %d, want 2", len(configs))
	}

	byName := map[string]*NativeConfig{}
	for _, c := range configs {
		byName[c.Name] = c
	}

	// User dir overrides system for same-named tool
	if byName["tool"].Description != "user-version" {
		t.Errorf("expected user-version, got %q", byName["tool"].Description)
	}
	// System-only tool still appears
	if byName["builtin"] == nil {
		t.Error("expected builtin tool from system dir")
	}
}

func TestEmptyMoxinPath(t *testing.T) {
	systemDir := filepath.Join(t.TempDir(), "system")
	writeDiscoveryMoxin(t, systemDir, "tool", "system")

	configs, err := DiscoverConfigs("", systemDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	if configs[0].Description != "system" {
		t.Errorf("expected system, got %q", configs[0].Description)
	}
}

func TestEmptyMoxinPathNoSystemDir(t *testing.T) {
	configs, err := DiscoverConfigs("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 0 {
		t.Fatalf("len(configs) = %d, want 0", len(configs))
	}
}

func TestDiscoverConfigsBrokenSymlink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "moxins")
	writeDiscoveryMoxin(t, dir, "valid-tool", "valid")

	// Broken symlink pointing to a non-existent target
	os.Symlink(
		filepath.Join(t.TempDir(), "nonexistent", "ghost"),
		filepath.Join(dir, "broken"),
	)

	configs, err := DiscoverConfigs(dir, "")
	if err != nil {
		t.Fatalf("broken symlink caused discovery to fail: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	if configs[0].Name != "valid-tool" {
		t.Errorf("expected valid-tool, got %q", configs[0].Name)
	}
}

func TestParseMoxinPath(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"/a:/b:/c", []string{"/a", "/b", "/c"}},
		{"/a::/b", []string{"/a", "/b"}},           // empty entry skipped
		{"  /a  : /b ", []string{"/a", "/b"}},       // whitespace trimmed
		{"/single", []string{"/single"}},
	}

	for _, tc := range tests {
		got := ParseMoxinPath(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("ParseMoxinPath(%q) = %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ParseMoxinPath(%q)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

func TestDefaultMoxinPath(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "projects", "myapp")

	// Create directories that should appear in the default path
	localMoxins := filepath.Join(cwd, ".moxy", "moxins")
	intermediateMoxins := filepath.Join(home, "projects", ".moxy", "moxins")
	globalMoxins := filepath.Join(home, ".config", "moxy", "moxins")
	systemDir := filepath.Join(t.TempDir(), "share", "moxy", "moxins")

	os.MkdirAll(localMoxins, 0o755)
	os.MkdirAll(intermediateMoxins, 0o755)
	os.MkdirAll(globalMoxins, 0o755)
	os.MkdirAll(systemDir, 0o755)

	got := DefaultMoxinPath(home, cwd, systemDir)
	parts := strings.Split(got, ":")

	if len(parts) != 4 {
		t.Fatalf("expected 4 parts, got %d: %q", len(parts), got)
	}

	// Priority order: local > intermediate > global > system
	if parts[0] != localMoxins {
		t.Errorf("parts[0] = %q, want %q (local)", parts[0], localMoxins)
	}
	if parts[1] != intermediateMoxins {
		t.Errorf("parts[1] = %q, want %q (intermediate)", parts[1], intermediateMoxins)
	}
	if parts[2] != globalMoxins {
		t.Errorf("parts[2] = %q, want %q (global)", parts[2], globalMoxins)
	}
	if parts[3] != systemDir {
		t.Errorf("parts[3] = %q, want %q (system)", parts[3], systemDir)
	}
}

func TestDefaultMoxinPathSkipsMissingDirs(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "project")
	os.MkdirAll(cwd, 0o755)

	// No .moxy/moxins dirs created → path should be empty
	got := DefaultMoxinPath(home, cwd, "/nonexistent/system")
	if got != "" {
		t.Errorf("expected empty path for missing dirs, got %q", got)
	}
}
