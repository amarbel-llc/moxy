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

func TestParseResourceTools(t *testing.T) {
	input := `
[[servers]]
name = "grit"
command = "grit"
generate-resource-tools = false
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].GenerateResourceTools == nil || *cfg.Servers[0].GenerateResourceTools != false {
		t.Error("expected generate-resource-tools = false")
	}
}

func TestParseResourceToolsDefault(t *testing.T) {
	input := `
[[servers]]
name = "grit"
command = "grit"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].GenerateResourceTools != nil {
		t.Error("expected resource_tools = nil (absent)")
	}
}

func TestParseAnnotationsSubTable(t *testing.T) {
	input := `
[[servers]]
name = "lux"
command = "lux"

[servers.annotations]
readOnlyHint = true
destructiveHint = false
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv := cfg.Servers[0]
	if srv.Annotations == nil {
		t.Fatal("expected annotations, got nil")
	}
	if srv.Annotations.ReadOnlyHint == nil || !*srv.Annotations.ReadOnlyHint {
		t.Error("expected readOnlyHint = true")
	}
	if srv.Annotations.DestructiveHint == nil || *srv.Annotations.DestructiveHint {
		t.Error("expected destructiveHint = false")
	}
	if srv.Annotations.IdempotentHint != nil {
		t.Error("expected idempotentHint = nil (absent)")
	}
}

func TestParseAnnotationsFlatMultiple(t *testing.T) {
	input := `
[[servers]]
name = "lux"
command = "lux"
readOnlyHint = true
idempotentHint = true
openWorldHint = false
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv := cfg.Servers[0]
	if srv.Annotations == nil {
		t.Fatal("expected annotations, got nil")
	}
	if srv.Annotations.ReadOnlyHint == nil || !*srv.Annotations.ReadOnlyHint {
		t.Error("expected readOnlyHint = true")
	}
	if srv.Annotations.IdempotentHint == nil || !*srv.Annotations.IdempotentHint {
		t.Error("expected idempotentHint = true")
	}
	if srv.Annotations.OpenWorldHint == nil || *srv.Annotations.OpenWorldHint {
		t.Error("expected openWorldHint = false")
	}
	if srv.Annotations.DestructiveHint != nil {
		t.Error("expected destructiveHint = nil (absent)")
	}
}

func TestParseAnnotationsAbsent(t *testing.T) {
	input := `
[[servers]]
name = "grit"
command = "grit"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].Annotations != nil {
		t.Error("expected nil annotations when none specified")
	}
}

func TestParseAllFields(t *testing.T) {
	input := `
[[servers]]
name = "grit"
command = "grit mcp --verbose"
paginate = true
generate-resource-tools = false
readOnlyHint = true
destructiveHint = false
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv := cfg.Servers[0]
	if srv.Name != "grit" {
		t.Errorf("name: got %q", srv.Name)
	}
	if srv.Command.Executable() != "grit" {
		t.Errorf("executable: got %q", srv.Command.Executable())
	}
	if !srv.Paginate {
		t.Error("expected paginate = true")
	}
	if srv.GenerateResourceTools == nil || *srv.GenerateResourceTools {
		t.Error("expected generate-resource-tools = false")
	}
	if srv.Annotations == nil || srv.Annotations.ReadOnlyHint == nil || !*srv.Annotations.ReadOnlyHint {
		t.Error("expected readOnlyHint = true")
	}
	if srv.Annotations.DestructiveHint == nil || *srv.Annotations.DestructiveHint {
		t.Error("expected destructiveHint = false")
	}
}

func TestParseNoCommand(t *testing.T) {
	input := `
[[servers]]
name = "broken"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Servers[0].Command.IsEmpty() {
		t.Error("expected empty command when not specified")
	}
}

func makeCommand(parts ...string) Command {
	return Command{parts: parts}
}

func boolPtr(b bool) *bool { return &b }

func TestParseEphemeralGlobal(t *testing.T) {
	input := `
ephemeral = true

[[servers]]
name = "echo"
command = "echo"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ephemeral == nil || !*cfg.Ephemeral {
		t.Error("expected global ephemeral = true")
	}
}

func TestParseEphemeralPerServer(t *testing.T) {
	input := `
[[servers]]
name = "echo"
command = "echo"
ephemeral = true
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].Ephemeral == nil || !*cfg.Servers[0].Ephemeral {
		t.Error("expected server ephemeral = true")
	}
}

func TestIsEphemeralInheritance(t *testing.T) {
	tests := []struct {
		name   string
		global *bool
		server *bool
		want   bool
	}{
		{"both nil", nil, nil, false},
		{"global true, server nil", boolPtr(true), nil, true},
		{"global false, server nil", boolPtr(false), nil, false},
		{"global nil, server true", nil, boolPtr(true), true},
		{"global true, server false", boolPtr(true), boolPtr(false), false},
		{"global false, server true", boolPtr(false), boolPtr(true), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ServerConfig{Ephemeral: tt.server}
			if got := s.IsEphemeral(tt.global); got != tt.want {
				t.Errorf("IsEphemeral() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeEphemeral(t *testing.T) {
	base := Config{Ephemeral: boolPtr(false)}
	overlay := Config{Ephemeral: boolPtr(true)}
	merged := Merge(base, overlay)
	if merged.Ephemeral == nil || !*merged.Ephemeral {
		t.Error("expected overlay ephemeral to win")
	}

	// nil overlay should preserve base
	merged2 := Merge(base, Config{})
	if merged2.Ephemeral == nil || *merged2.Ephemeral {
		t.Error("expected base ephemeral to be preserved")
	}
}

func TestParseProgressiveDisclosureGlobal(t *testing.T) {
	input := `
progressive-disclosure = true

[[servers]]
name = "echo"
command = "echo"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ProgressiveDisclosure == nil || !*cfg.ProgressiveDisclosure {
		t.Error("expected global progressive-disclosure = true")
	}
}

func TestParseProgressiveDisclosurePerServer(t *testing.T) {
	input := `
[[servers]]
name = "echo"
command = "echo"
progressive-disclosure = true
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].ProgressiveDisclosure == nil || !*cfg.Servers[0].ProgressiveDisclosure {
		t.Error("expected server progressive-disclosure = true")
	}
}

func TestIsProgressiveDisclosureInheritance(t *testing.T) {
	tests := []struct {
		name   string
		global *bool
		server *bool
		want   bool
	}{
		{"both nil", nil, nil, false},
		{"global true, server nil", boolPtr(true), nil, true},
		{"global false, server nil", boolPtr(false), nil, false},
		{"global nil, server true", nil, boolPtr(true), true},
		{"global true, server false", boolPtr(true), boolPtr(false), false},
		{"global false, server true", boolPtr(false), boolPtr(true), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ServerConfig{ProgressiveDisclosure: tt.server}
			if got := s.IsProgressiveDisclosure(tt.global); got != tt.want {
				t.Errorf("IsProgressiveDisclosure() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeProgressiveDisclosure(t *testing.T) {
	base := Config{ProgressiveDisclosure: boolPtr(false)}
	overlay := Config{ProgressiveDisclosure: boolPtr(true)}
	merged := Merge(base, overlay)
	if merged.ProgressiveDisclosure == nil || !*merged.ProgressiveDisclosure {
		t.Error("expected overlay progressive-disclosure to win")
	}

	merged2 := Merge(base, Config{})
	if merged2.ProgressiveDisclosure == nil || *merged2.ProgressiveDisclosure {
		t.Error("expected base progressive-disclosure to be preserved")
	}
}

func TestParseRejectsDotInServerName(t *testing.T) {
	input := `
[[servers]]
name = "my.server"
command = "echo"
`
	_, err := Parse([]byte(input))
	if err == nil {
		t.Fatal("expected error for server name containing dot")
	}
}

func TestParseAllowsHyphenInServerName(t *testing.T) {
	input := `
[[servers]]
name = "my-server"
command = "echo"
`
	_, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseNixDevshell(t *testing.T) {
	input := `
[[servers]]
name = "srv"
command = "manpage"
nix-devshell = "."
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Servers[0].NixDevshell == nil || *cfg.Servers[0].NixDevshell != "." {
		t.Errorf("nix-devshell: got %v", cfg.Servers[0].NixDevshell)
	}
}

func TestEffectiveCommandWithoutDevshell(t *testing.T) {
	srv := ServerConfig{Command: makeCommand("manpage", "--verbose")}
	exe, args := srv.EffectiveCommand()
	if exe != "manpage" {
		t.Errorf("exe: got %q, want %q", exe, "manpage")
	}
	if len(args) != 1 || args[0] != "--verbose" {
		t.Errorf("args: got %v, want [--verbose]", args)
	}
}

func TestEffectiveCommandWithDevshell(t *testing.T) {
	ref := "."
	srv := ServerConfig{
		Command:     makeCommand("manpage", "--verbose"),
		NixDevshell: &ref,
	}
	exe, args := srv.EffectiveCommand()
	if exe != "nix" {
		t.Errorf("exe: got %q, want %q", exe, "nix")
	}
	want := []string{"develop", ".", "--command", "manpage", "--verbose"}
	if len(args) != len(want) {
		t.Fatalf("args length: got %d, want %d", len(args), len(want))
	}
	for i, w := range want {
		if args[i] != w {
			t.Errorf("args[%d]: got %q, want %q", i, args[i], w)
		}
	}
}

func TestEffectiveCommandWithFlakeRef(t *testing.T) {
	ref := "github:amarbel-llc/eng?dir=devenvs/go"
	srv := ServerConfig{
		Command:     makeCommand("grit"),
		NixDevshell: &ref,
	}
	exe, args := srv.EffectiveCommand()
	if exe != "nix" {
		t.Errorf("exe: got %q, want %q", exe, "nix")
	}
	if args[1] != ref {
		t.Errorf("flake ref: got %q, want %q", args[1], ref)
	}
}

func TestEffectiveCommandDoesNotExpandEnvVars(t *testing.T) {
	t.Setenv("HOME", "/Users/testuser")

	srv := ServerConfig{Command: makeCommand("$HOME/bin/my-server")}
	exe, _ := srv.EffectiveCommand()

	want := "/Users/testuser/bin/my-server"
	if exe == "$HOME/bin/my-server" {
		t.Errorf("exe was not expanded: got literal %q, want %q", exe, want)
	}
}

func TestEffectiveCommandDoesNotExpandTilde(t *testing.T) {
	t.Setenv("HOME", "/Users/testuser")

	srv := ServerConfig{Command: makeCommand("~/bin/my-server")}
	exe, _ := srv.EffectiveCommand()

	want := "/Users/testuser/bin/my-server"
	if exe == "~/bin/my-server" {
		t.Errorf("exe was not expanded: got literal %q, want %q", exe, want)
	}
}

func TestParseCommandDoesNotExpandEnvVars(t *testing.T) {
	t.Setenv("HOME", "/Users/testuser")

	data := []byte(`
[[servers]]
name = "test"
command = "$HOME/bin/my-server"
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}

	exe, _ := cfg.Servers[0].EffectiveCommand()

	want := "/Users/testuser/bin/my-server"
	if exe == "$HOME/bin/my-server" {
		t.Errorf("exe was not expanded during parse: got literal %q, want %q", exe, want)
	}
}
