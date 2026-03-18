package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMinimal(t *testing.T) {
	input := `
[servers.echo]
command = "echo"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv, ok := cfg.Servers["echo"]
	if !ok {
		t.Fatal("expected server 'echo'")
	}
	if srv.Command != "echo" {
		t.Errorf("command: got %q", srv.Command)
	}
}

func TestParseEmpty(t *testing.T) {
	cfg, err := Parse([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers != nil {
		t.Errorf("expected nil servers, got %v", cfg.Servers)
	}
}

func TestLoadFromPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moxyfile")
	os.WriteFile(path, []byte(`
[servers.echo]
command = "echo"
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := cfg.Servers["echo"]; !ok {
		t.Error("expected server 'echo'")
	}
}

func TestLoadMissing(t *testing.T) {
	cfg, err := Load("/nonexistent/moxyfile")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers != nil {
		t.Errorf("expected nil servers, got %v", cfg.Servers)
	}
}

func TestMergeAddsNewServer(t *testing.T) {
	base := Config{Servers: map[string]ServerConfig{
		"grit": {Command: "grit"},
	}}
	repo := Config{Servers: map[string]ServerConfig{
		"lux": {Command: "lux"},
	}}
	merged := Merge(base, repo)
	if len(merged.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(merged.Servers))
	}
	if merged.Servers["grit"].Command != "grit" {
		t.Error("expected grit inherited")
	}
	if merged.Servers["lux"].Command != "lux" {
		t.Error("expected lux added")
	}
}

func TestMergeOverridesServer(t *testing.T) {
	base := Config{Servers: map[string]ServerConfig{
		"grit": {Command: "grit", Args: []string{"mcp"}},
	}}
	repo := Config{Servers: map[string]ServerConfig{
		"grit": {Command: "grit", Args: []string{"mcp", "--verbose"}},
	}}
	merged := Merge(base, repo)
	if len(merged.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(merged.Servers))
	}
	if len(merged.Servers["grit"].Args) != 2 || merged.Servers["grit"].Args[1] != "--verbose" {
		t.Errorf("expected overridden args, got %v", merged.Servers["grit"].Args)
	}
}

func TestMergeBaseOnly(t *testing.T) {
	base := Config{Servers: map[string]ServerConfig{
		"grit": {Command: "grit"},
	}}
	merged := Merge(base, Config{})
	if len(merged.Servers) != 1 || merged.Servers["grit"].Command != "grit" {
		t.Errorf("expected inherited server, got %v", merged.Servers)
	}
}

func TestMergeRepoOnly(t *testing.T) {
	repo := Config{Servers: map[string]ServerConfig{
		"grit": {Command: "grit"},
	}}
	merged := Merge(Config{}, repo)
	if len(merged.Servers) != 1 || merged.Servers["grit"].Command != "grit" {
		t.Errorf("expected repo server, got %v", merged.Servers)
	}
}

func TestMergeBothEmpty(t *testing.T) {
	merged := Merge(Config{}, Config{})
	if merged.Servers != nil {
		t.Errorf("expected nil servers, got %v", merged.Servers)
	}
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestLoadHierarchyGlobalOnly(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "eng", "repos", "myrepo")
	os.MkdirAll(repoDir, 0o755)

	writeConfig(t, filepath.Join(home, ".config", "moxy", "moxyfile"), `
[servers.grit]
command = "grit"
`)

	result, err := LoadHierarchy(home, repoDir)
	if err != nil {
		t.Fatalf("LoadHierarchy returned error: %v", err)
	}

	// Sources: global, eng/moxyfile, eng/repos/moxyfile, myrepo/moxyfile
	if len(result.Sources) != 4 {
		t.Fatalf("expected 4 sources, got %d", len(result.Sources))
	}
	if !result.Sources[0].Found {
		t.Error("expected global source to be found")
	}
	for i := 1; i < len(result.Sources); i++ {
		if result.Sources[i].Found {
			t.Errorf("expected source %d (%s) to not be found", i, result.Sources[i].Path)
		}
	}
	if result.Merged.Servers["grit"].Command != "grit" {
		t.Errorf("expected grit server, got %v", result.Merged.Servers)
	}
}

func TestLoadHierarchyGlobalAndRepo(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "eng", "repos", "myrepo")
	os.MkdirAll(repoDir, 0o755)

	writeConfig(t, filepath.Join(home, ".config", "moxy", "moxyfile"), `
[servers.grit]
command = "grit"
args = ["mcp"]
`)
	writeConfig(t, filepath.Join(repoDir, "moxyfile"), `
[servers.grit]
command = "grit"
args = ["mcp", "--verbose"]

[servers.lux]
command = "lux"
`)

	result, err := LoadHierarchy(home, repoDir)
	if err != nil {
		t.Fatalf("LoadHierarchy returned error: %v", err)
	}

	if len(result.Merged.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(result.Merged.Servers))
	}
	if len(result.Merged.Servers["grit"].Args) != 2 || result.Merged.Servers["grit"].Args[1] != "--verbose" {
		t.Errorf("expected overridden grit args, got %v", result.Merged.Servers["grit"].Args)
	}
	if result.Merged.Servers["lux"].Command != "lux" {
		t.Error("expected lux server added by repo")
	}
}

func TestLoadHierarchyParentDir(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "eng", "repos", "myrepo")
	os.MkdirAll(repoDir, 0o755)

	writeConfig(t, filepath.Join(home, "eng", "moxyfile"), `
[servers.grit]
command = "grit"
`)
	writeConfig(t, filepath.Join(repoDir, "moxyfile"), `
[servers.lux]
command = "lux"
`)

	result, err := LoadHierarchy(home, repoDir)
	if err != nil {
		t.Fatalf("LoadHierarchy returned error: %v", err)
	}

	if len(result.Merged.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %v", result.Merged.Servers)
	}
	if !result.Sources[1].Found {
		t.Error("expected eng/moxyfile source to be found")
	}
}

func TestLoadHierarchyNoFiles(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "eng", "repos", "myrepo")
	os.MkdirAll(repoDir, 0o755)

	result, err := LoadHierarchy(home, repoDir)
	if err != nil {
		t.Fatalf("LoadHierarchy returned error: %v", err)
	}

	for i, src := range result.Sources {
		if src.Found {
			t.Errorf("expected source %d (%s) to not be found", i, src.Path)
		}
	}
	if result.Merged.Servers != nil {
		t.Errorf("expected nil servers, got %v", result.Merged.Servers)
	}
}
