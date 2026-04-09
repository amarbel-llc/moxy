package native

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverConfigs(t *testing.T) {
	// Create a temp hierarchy:
	// home/.config/moxy/.moxy/global.toml
	// home/project/.moxy/local.toml
	home := t.TempDir()
	project := filepath.Join(home, "project")

	globalMoxy := filepath.Join(home, ".config", "moxy", ".moxy")
	os.MkdirAll(globalMoxy, 0o755)
	os.WriteFile(filepath.Join(globalMoxy, "global.toml"), []byte(`
name = "global-tool"
[[tools]]
name = "hello"
command = "echo"
args = ["hello"]
`), 0o644)

	localMoxy := filepath.Join(project, ".moxy")
	os.MkdirAll(localMoxy, 0o755)
	os.WriteFile(filepath.Join(localMoxy, "local.toml"), []byte(`
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

func TestDiscoverConfigsOverride(t *testing.T) {
	// Later .moxy/ directory overrides earlier by server name
	home := t.TempDir()
	project := filepath.Join(home, "project")

	globalMoxy := filepath.Join(home, ".config", "moxy", ".moxy")
	os.MkdirAll(globalMoxy, 0o755)
	os.WriteFile(filepath.Join(globalMoxy, "shell.toml"), []byte(`
name = "shell"
description = "global"
[[tools]]
name = "exec"
command = "sh"
`), 0o644)

	localMoxy := filepath.Join(project, ".moxy")
	os.MkdirAll(localMoxy, 0o755)
	os.WriteFile(filepath.Join(localMoxy, "shell.toml"), []byte(`
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
