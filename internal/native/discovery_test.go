package native

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const testToolConfig = `
name = "%s"
description = "%s"
[[tools]]
name = "hello"
command = "echo"
args = ["hello"]
`

func writeTestConfig(t *testing.T, dir, filename, name, desc string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	data := []byte(fmt.Sprintf(testToolConfig, name, desc))
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverConfigs(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")

	globalServers := filepath.Join(home, ".config", "moxy", "servers")
	writeTestConfig(t, globalServers, "global.toml", "global-tool", "global")

	localServers := filepath.Join(project, ".moxy", "servers")
	writeTestConfig(t, localServers, "local.toml", "local-tool", "local")

	configs, err := DiscoverConfigs("", home, project)
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
	if !names["global-tool"] {
		t.Error("expected global-tool in discovered configs")
	}
	if !names["local-tool"] {
		t.Error("expected local-tool in discovered configs")
	}
}

func TestDiscoverConfigsBrokenSymlink(t *testing.T) {
	// A broken symlink in the servers/ directory should not prevent
	// other valid configs from being discovered.
	home := t.TempDir()
	project := filepath.Join(home, "project")

	globalServers := filepath.Join(home, ".config", "moxy", "servers")
	writeTestConfig(t, globalServers, "valid.toml", "valid-tool", "valid")

	// Broken symlink pointing to a non-existent target
	os.Symlink(
		filepath.Join(home, "nonexistent", "ghost.toml"),
		filepath.Join(globalServers, "broken.toml"),
	)

	os.MkdirAll(project, 0o755)

	configs, err := DiscoverConfigs("", home, project)
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

func TestDiscoverConfigsOverride(t *testing.T) {
	// Later servers/ directory overrides earlier by server name
	home := t.TempDir()
	project := filepath.Join(home, "project")

	globalServers := filepath.Join(home, ".config", "moxy", "servers")
	writeTestConfig(t, globalServers, "shell.toml", "shell", "global")

	localServers := filepath.Join(project, ".moxy", "servers")
	writeTestConfig(t, localServers, "shell.toml", "shell", "local")

	configs, err := DiscoverConfigs("", home, project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	if configs[0].Description != "local" {
		t.Errorf("expected local override, got description=%q", configs[0].Description)
	}
}

func TestDiscoverConfigsBuiltinLayer(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	os.MkdirAll(project, 0o755)

	builtinDir := filepath.Join(t.TempDir(), "share", "moxy", "builtin-servers")
	writeTestConfig(t, builtinDir, "builtin.toml", "builtin-tool", "builtin")

	configs, err := DiscoverConfigs(builtinDir, home, project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	if configs[0].Name != "builtin-tool" {
		t.Errorf("expected builtin-tool, got %q", configs[0].Name)
	}
}

func TestDiscoverConfigsBuiltinOverriddenByGlobal(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	os.MkdirAll(project, 0o755)

	builtinDir := filepath.Join(t.TempDir(), "share", "moxy", "builtin-servers")
	writeTestConfig(t, builtinDir, "tool.toml", "tool", "builtin")

	globalServers := filepath.Join(home, ".config", "moxy", "servers")
	writeTestConfig(t, globalServers, "tool.toml", "tool", "global-override")

	configs, err := DiscoverConfigs(builtinDir, home, project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	if configs[0].Description != "global-override" {
		t.Errorf("expected global-override, got description=%q", configs[0].Description)
	}
}

func TestDiscoverConfigsBuiltinOverriddenByLocal(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")

	builtinDir := filepath.Join(t.TempDir(), "share", "moxy", "builtin-servers")
	writeTestConfig(t, builtinDir, "tool.toml", "tool", "builtin")

	localServers := filepath.Join(project, ".moxy", "servers")
	writeTestConfig(t, localServers, "tool.toml", "tool", "local-override")

	configs, err := DiscoverConfigs(builtinDir, home, project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	if configs[0].Description != "local-override" {
		t.Errorf("expected local-override, got description=%q", configs[0].Description)
	}
}

func TestBuiltinDirEnvOverride(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "custom-builtins")
	os.MkdirAll(dir, 0o755)

	t.Setenv("MOXY_BUILTIN_DIR", dir)
	got := BuiltinDir()
	if got != dir {
		t.Errorf("BuiltinDir() = %q, want %q", got, dir)
	}
}

func TestBuiltinDirMissing(t *testing.T) {
	t.Setenv("MOXY_BUILTIN_DIR", "/nonexistent/path/that/does/not/exist")
	got := BuiltinDir()
	if got != "" {
		t.Errorf("BuiltinDir() = %q, want empty string for missing dir", got)
	}
}

func TestDiscoverConfigsEmptyBuiltinDir(t *testing.T) {
	// Empty builtinDir should behave like before (no builtins)
	home := t.TempDir()
	project := filepath.Join(home, "project")

	globalServers := filepath.Join(home, ".config", "moxy", "servers")
	writeTestConfig(t, globalServers, "tool.toml", "tool", "global")

	os.MkdirAll(project, 0o755)

	configs, err := DiscoverConfigs("", home, project)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("len(configs) = %d, want 1", len(configs))
	}
	if configs[0].Description != "global" {
		t.Errorf("expected global, got description=%q", configs[0].Description)
	}
}
