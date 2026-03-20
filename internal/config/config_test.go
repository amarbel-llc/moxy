package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMinimal(t *testing.T) {
	input := `
[[servers]]
name = "echo"
command = "echo"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "echo" {
		t.Errorf("name: got %q", cfg.Servers[0].Name)
	}
	if cfg.Servers[0].Command.Executable() != "echo" {
		t.Errorf("command: got %q", cfg.Servers[0].Command.Executable())
	}
}

func TestParseCommandString(t *testing.T) {
	input := `
[[servers]]
name = "grit"
command = "grit mcp --verbose"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv := cfg.Servers[0]
	if srv.Command.Executable() != "grit" {
		t.Errorf("executable: got %q", srv.Command.Executable())
	}
	args := srv.Command.Args()
	if len(args) != 2 || args[0] != "mcp" || args[1] != "--verbose" {
		t.Errorf("args: got %v", args)
	}
}

func TestParseCommandArray(t *testing.T) {
	input := `
[[servers]]
name = "lux"
command = ["lux", "--lsp-dir", "/path/to/lsps"]
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv := cfg.Servers[0]
	if srv.Command.Executable() != "lux" {
		t.Errorf("executable: got %q", srv.Command.Executable())
	}
	args := srv.Command.Args()
	if len(args) != 2 || args[0] != "--lsp-dir" || args[1] != "/path/to/lsps" {
		t.Errorf("args: got %v", args)
	}
}

func TestParseMultipleServers(t *testing.T) {
	input := `
[[servers]]
name = "grit"
command = "grit mcp"

[[servers]]
name = "lux"
command = ["lux", "--lsp-dir", "/tmp"]
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(cfg.Servers))
	}
	if cfg.Servers[0].Name != "grit" || cfg.Servers[1].Name != "lux" {
		t.Errorf("names: got %q, %q", cfg.Servers[0].Name, cfg.Servers[1].Name)
	}
}

func TestParseAnnotationsFlat(t *testing.T) {
	input := `
[[servers]]
name = "lux"
command = "lux"
readOnlyHint = true
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv := cfg.Servers[0]
	if srv.Annotations == nil || srv.Annotations.ReadOnlyHint == nil ||
		!*srv.Annotations.ReadOnlyHint {
		t.Error("expected readOnlyHint = true")
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
[[servers]]
name = "echo"
command = "echo"
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "echo" {
		t.Errorf("expected server 'echo', got %v", cfg.Servers)
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
	base := Config{Servers: []ServerConfig{
		{Name: "grit", Command: makeCommand("grit")},
	}}
	repo := Config{Servers: []ServerConfig{
		{Name: "lux", Command: makeCommand("lux")},
	}}
	merged := Merge(base, repo)
	if len(merged.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(merged.Servers))
	}
	if merged.Servers[0].Name != "grit" || merged.Servers[1].Name != "lux" {
		t.Errorf(
			"names: got %q, %q",
			merged.Servers[0].Name,
			merged.Servers[1].Name,
		)
	}
}

func TestMergeOverridesServer(t *testing.T) {
	base := Config{Servers: []ServerConfig{
		{Name: "grit", Command: makeCommand("grit", "mcp")},
	}}
	repo := Config{Servers: []ServerConfig{
		{Name: "grit", Command: makeCommand("grit", "mcp", "--verbose")},
	}}
	merged := Merge(base, repo)
	if len(merged.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(merged.Servers))
	}
	args := merged.Servers[0].Command.Args()
	if len(args) != 2 || args[1] != "--verbose" {
		t.Errorf("expected overridden args, got %v", args)
	}
}

func TestMergeBaseOnly(t *testing.T) {
	base := Config{Servers: []ServerConfig{
		{Name: "grit", Command: makeCommand("grit")},
	}}
	merged := Merge(base, Config{})
	if len(merged.Servers) != 1 || merged.Servers[0].Name != "grit" {
		t.Errorf("expected inherited server, got %v", merged.Servers)
	}
}

func TestMergeRepoOnly(t *testing.T) {
	repo := Config{Servers: []ServerConfig{
		{Name: "grit", Command: makeCommand("grit")},
	}}
	merged := Merge(Config{}, repo)
	if len(merged.Servers) != 1 || merged.Servers[0].Name != "grit" {
		t.Errorf("expected repo server, got %v", merged.Servers)
	}
}

func TestMergeBothEmpty(t *testing.T) {
	merged := Merge(Config{}, Config{})
	if len(merged.Servers) != 0 {
		t.Errorf("expected no servers, got %v", merged.Servers)
	}
}

func TestMergePreservesOrder(t *testing.T) {
	base := Config{Servers: []ServerConfig{
		{Name: "alpha", Command: makeCommand("alpha")},
		{Name: "beta", Command: makeCommand("beta")},
	}}
	repo := Config{Servers: []ServerConfig{
		{Name: "gamma", Command: makeCommand("gamma")},
		{Name: "alpha", Command: makeCommand("alpha-v2")},
	}}
	merged := Merge(base, repo)
	if len(merged.Servers) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(merged.Servers))
	}
	// alpha overridden in place, beta inherited, gamma appended
	if merged.Servers[0].Name != "alpha" ||
		merged.Servers[0].Command.Executable() != "alpha-v2" {
		t.Errorf("expected alpha-v2 at index 0, got %v", merged.Servers[0])
	}
	if merged.Servers[1].Name != "beta" {
		t.Errorf("expected beta at index 1, got %v", merged.Servers[1])
	}
	if merged.Servers[2].Name != "gamma" {
		t.Errorf("expected gamma at index 2, got %v", merged.Servers[2])
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
[[servers]]
name = "grit"
command = "grit"
`)

	result, err := LoadHierarchy(home, repoDir)
	if err != nil {
		t.Fatalf("LoadHierarchy returned error: %v", err)
	}

	if len(result.Sources) != 4 {
		t.Fatalf("expected 4 sources, got %d", len(result.Sources))
	}
	if !result.Sources[0].Found {
		t.Error("expected global source to be found")
	}
	for i := 1; i < len(result.Sources); i++ {
		if result.Sources[i].Found {
			t.Errorf(
				"expected source %d (%s) to not be found",
				i,
				result.Sources[i].Path,
			)
		}
	}
	if len(result.Merged.Servers) != 1 ||
		result.Merged.Servers[0].Command.Executable() != "grit" {
		t.Errorf("expected grit server, got %v", result.Merged.Servers)
	}
}

func TestLoadHierarchyGlobalAndRepo(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "eng", "repos", "myrepo")
	os.MkdirAll(repoDir, 0o755)

	writeConfig(t, filepath.Join(home, ".config", "moxy", "moxyfile"), `
[[servers]]
name = "grit"
command = "grit mcp"
`)
	writeConfig(t, filepath.Join(repoDir, "moxyfile"), `
[[servers]]
name = "grit"
command = "grit mcp --verbose"

[[servers]]
name = "lux"
command = "lux"
`)

	result, err := LoadHierarchy(home, repoDir)
	if err != nil {
		t.Fatalf("LoadHierarchy returned error: %v", err)
	}

	if len(result.Merged.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(result.Merged.Servers))
	}
	gritArgs := result.Merged.Servers[0].Command.Args()
	if len(gritArgs) != 2 || gritArgs[1] != "--verbose" {
		t.Errorf("expected overridden grit args, got %v", gritArgs)
	}
	if result.Merged.Servers[1].Name != "lux" {
		t.Error("expected lux server added by repo")
	}
}

func TestLoadHierarchyParentDir(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "eng", "repos", "myrepo")
	os.MkdirAll(repoDir, 0o755)

	writeConfig(t, filepath.Join(home, "eng", "moxyfile"), `
[[servers]]
name = "grit"
command = "grit"
`)
	writeConfig(t, filepath.Join(repoDir, "moxyfile"), `
[[servers]]
name = "lux"
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
	if len(result.Merged.Servers) != 0 {
		t.Errorf("expected no servers, got %v", result.Merged.Servers)
	}
}

func TestCommandString(t *testing.T) {
	tests := []struct {
		name   string
		parts  []string
		expect string
	}{
		{"single word", []string{"grit"}, "grit"},
		{
			"multiple words",
			[]string{"grit", "mcp", "--verbose"},
			"grit mcp --verbose",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := makeCommand(tt.parts...)
			if got := cmd.String(); got != tt.expect {
				t.Errorf("got %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestParsePaginate(t *testing.T) {
	input := `
[[servers]]
name = "caldav"
command = "caldav-mcp"
paginate = true
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Servers[0].Paginate {
		t.Error("expected paginate = true")
	}
}

func TestParsePaginateDefault(t *testing.T) {
	input := `
[[servers]]
name = "grit"
command = "grit"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].Paginate {
		t.Error("expected paginate = false by default")
	}
}

func makeCommand(parts ...string) Command {
	return Command{parts: parts}
}
