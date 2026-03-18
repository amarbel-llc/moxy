# Moxyfile Hierarchy Merge Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Load and merge moxyfiles from a hierarchy of locations (global, intermediate dirs, repo-local) so users can define shared MCP server defaults globally and override per-repo.

**Architecture:** Extract `Parse` and `Load` (silent-miss) from the existing `config.Load`, add a `Merge` function for last-writer-wins server maps, and walk the directory hierarchy in `LoadHierarchy`. The caller (`main.go`) switches to `LoadDefaultHierarchy` and validates the merged result.

**Tech Stack:** Go, `github.com/BurntSushi/toml`, stdlib `os`, `path/filepath`, `errors/fs`

**Rollback:** N/A — purely additive. Existing single-moxyfile usage is a degenerate case of the hierarchy (only layer 3 exists).

---

### Task 1: Extract Parse from Load

**Files:**
- Modify: `internal/config/config.go:27-49`
- Create: `internal/config/config_test.go`

**Step 1: Write the failing test**

In `internal/config/config_test.go`:

```go
package config

import "testing"

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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestParse ./internal/config/ -v`
Expected: FAIL — `Parse` is not defined

**Step 3: Write minimal implementation**

Add to `internal/config/config.go`:

```go
func Parse(data []byte) (Config, error) {
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing moxyfile: %w", err)
	}
	return cfg, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestParse ./internal/config/ -v`
Expected: PASS

**Step 5: Commit**

```
feat(config): extract Parse function from Load
```

---

### Task 2: Extract Load to silently skip missing files

**Files:**
- Modify: `internal/config/config.go:27-49`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
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
```

Add imports: `"os"`, `"path/filepath"`.

**Step 2: Run test to verify it fails**

Run: `go test -run TestLoad ./internal/config/ -v`
Expected: FAIL — current `Load` returns error on missing file and returns `*Config` not `Config`

**Step 3: Rewrite Load**

Replace the existing `Load` function in `internal/config/config.go`:

```go
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("reading moxyfile: %w", err)
	}
	return Parse(data)
}
```

Add imports: `"errors"`, `"io/fs"`.

Remove the old validation logic from `Load` (server count check, command check) — that moves to the caller in Task 5.

**Step 4: Run test to verify it passes**

Run: `go test -run TestLoad ./internal/config/ -v`
Expected: PASS

**Step 5: Commit**

```
feat(config): rewrite Load to silently skip missing files
```

---

### Task 3: Add Merge function

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestMerge ./internal/config/ -v`
Expected: FAIL — `Merge` is not defined

**Step 3: Write minimal implementation**

Add to `internal/config/config.go`:

```go
func Merge(base, overlay Config) Config {
	merged := base

	if overlay.Servers != nil {
		if merged.Servers == nil {
			merged.Servers = make(map[string]ServerConfig)
		}
		for name, srv := range overlay.Servers {
			merged.Servers[name] = srv
		}
	}

	return merged
}
```

**Step 4: Run test to verify it passes**

Run: `go test -run TestMerge ./internal/config/ -v`
Expected: PASS

**Step 5: Commit**

```
feat(config): add Merge with last-writer-wins per server name
```

---

### Task 4: Add LoadHierarchy and LoadDefaultHierarchy

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test -run TestLoadHierarchy ./internal/config/ -v`
Expected: FAIL — `LoadHierarchy` is not defined

**Step 3: Write implementation**

Add types and functions to `internal/config/config.go`:

```go
type LoadSource struct {
	Path  string
	Found bool
	File  Config
}

type Hierarchy struct {
	Sources []LoadSource
	Merged  Config
}

func LoadDefaultHierarchy() (Hierarchy, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Hierarchy{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return Hierarchy{}, err
	}

	return LoadHierarchy(home, cwd)
}

func LoadHierarchy(home, dir string) (Hierarchy, error) {
	var sources []LoadSource
	merged := Config{}

	loadAndMerge := func(path string) error {
		cfg, err := Load(path)
		if err != nil {
			return err
		}
		_, found := fileExists(path)
		sources = append(sources, LoadSource{Path: path, Found: found, File: cfg})
		if found {
			merged = Merge(merged, cfg)
		}
		return nil
	}

	// 1. Global config
	globalPath := filepath.Join(home, ".config", "moxy", "moxyfile")
	if err := loadAndMerge(globalPath); err != nil {
		return Hierarchy{}, err
	}

	// 2. Intermediate parent directories walking down from home to dir
	cleanHome := filepath.Clean(home)
	cleanDir := filepath.Clean(dir)

	rel, err := filepath.Rel(cleanHome, cleanDir)
	if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
		parts := strings.Split(rel, string(filepath.Separator))
		for i := 1; i < len(parts); i++ {
			parentDir := filepath.Join(cleanHome, filepath.Join(parts[:i]...))
			parentPath := filepath.Join(parentDir, "moxyfile")
			if err := loadAndMerge(parentPath); err != nil {
				return Hierarchy{}, err
			}
		}
	}

	// 3. Target directory moxyfile
	dirPath := filepath.Join(cleanDir, "moxyfile")
	if err := loadAndMerge(dirPath); err != nil {
		return Hierarchy{}, err
	}

	return Hierarchy{Sources: sources, Merged: merged}, nil
}

func fileExists(path string) (os.FileInfo, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	return info, true
}
```

Add imports: `"path/filepath"`, `"strings"`.

**Step 4: Run test to verify it passes**

Run: `go test -run TestLoadHierarchy ./internal/config/ -v`
Expected: PASS

**Step 5: Commit**

```
feat(config): add LoadHierarchy with source tracking
```

---

### Task 5: Switch main.go to LoadDefaultHierarchy

**Files:**
- Modify: `cmd/moxy/main.go:59-63`

**Step 1: Run existing tests to establish baseline**

Run: `go vet ./...`
Expected: PASS

**Step 2: Update runServer**

Replace lines 59-63 in `cmd/moxy/main.go`:

```go
// Before:
cfg, err := config.Load("moxyfile")
if err != nil {
	return err
}

// After:
hierarchy, err := config.LoadDefaultHierarchy()
if err != nil {
	return err
}

cfg := hierarchy.Merged

if len(cfg.Servers) == 0 {
	return fmt.Errorf("no servers configured in any moxyfile")
}

for name, srv := range cfg.Servers {
	if srv.Command == "" {
		return fmt.Errorf("server %q has no command", name)
	}
}
```

**Step 3: Run vet + build to verify**

Run: `go vet ./... && go build -o build/moxy ./cmd/moxy`
Expected: PASS, binary builds

**Step 4: Commit**

```
feat: switch to hierarchical moxyfile loading

Moxy now loads moxyfiles from ~/.config/moxy/moxyfile, intermediate
parent directories, and the current directory, merging them with
last-writer-wins per server name.
```

---

### Task 6: Update justfile test recipe

**Files:**
- Modify: `justfile:18-19`

**Step 1: Update test-go recipe**

Replace:
```
test-go:
  go vet ./...
```

With:
```
test-go:
  go vet ./...
  go test ./... -v
```

**Step 2: Run it**

Run: `just test-go`
Expected: All tests pass

**Step 3: Commit**

```
chore: add go test to test-go recipe
```
