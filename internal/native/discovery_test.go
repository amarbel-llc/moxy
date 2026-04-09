package native

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverConfigs(t *testing.T) {
	// Create a temp hierarchy:
	// home/.config/moxy/servers/global.toml
	// home/project/.moxy/servers/local.toml
	home := t.TempDir()
	project := filepath.Join(home, "project")

	globalServers := filepath.Join(home, ".config", "moxy", "servers")
	os.MkdirAll(globalServers, 0o755)
	os.WriteFile(filepath.Join(globalServers, "global.toml"), []byte(`
name = "global-tool"
[[tools]]
name = "hello"
command = "echo"
args = ["hello"]
`), 0o644)

	localServers := filepath.Join(project, ".moxy", "servers")
	os.MkdirAll(localServers, 0o755)
	os.WriteFile(filepath.Join(localServers, "local.toml"), []byte(`
name = "local-tool"
[[tools]]
name = "world"
command = "echo"
args = ["world"]
`), 0o644)

	configs, err := DiscoverConfigs(home, project)
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
	os.MkdirAll(globalServers, 0o755)

	// Valid config
	os.WriteFile(filepath.Join(globalServers, "valid.toml"), []byte(`
name = "valid-tool"
[[tools]]
name = "hello"
command = "echo"
args = ["hello"]
`), 0o644)

	// Broken symlink pointing to a non-existent target
	os.Symlink(
		filepath.Join(home, "nonexistent", "ghost.toml"),
		filepath.Join(globalServers, "broken.toml"),
	)

	os.MkdirAll(project, 0o755)

	configs, err := DiscoverConfigs(home, project)
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
	os.MkdirAll(globalServers, 0o755)
	os.WriteFile(filepath.Join(globalServers, "shell.toml"), []byte(`
name = "shell"
description = "global"
[[tools]]
name = "exec"
command = "sh"
`), 0o644)

	localServers := filepath.Join(project, ".moxy", "servers")
	os.MkdirAll(localServers, 0o755)
	os.WriteFile(filepath.Join(localServers, "shell.toml"), []byte(`
name = "shell"
description = "local"
[[tools]]
name = "exec"
command = "bash"
`), 0o644)

	configs, err := DiscoverConfigs(home, project)
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
