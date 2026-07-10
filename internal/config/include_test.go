package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// #407: an `include` directive pulls in an out-of-tree moxyfile and merges it
// at the includer's position — the include supplies defaults the including
// file (and the rest of the $HOME→CWD walk) can still override.

func TestLoadHierarchyIncludeMergesOutOfTreeConfig(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "eng", "repos", "myrepo")
	shared := filepath.Join(t.TempDir(), "shared-moxyfile")

	writeConfig(t, shared, `
[[servers]]
name = "grit"
command = "grit"
`)
	writeConfig(t, filepath.Join(repoDir, "moxyfile"), `
include = ["`+shared+`"]

[[servers]]
name = "lux"
command = "lux"
`)

	result, err := LoadHierarchy(home, repoDir)
	if err != nil {
		t.Fatalf("LoadHierarchy returned error: %v", err)
	}

	names := serverNames(result.Merged)
	if len(names) != 2 || names[0] != "grit" || names[1] != "lux" {
		t.Fatalf("expected [grit lux] (include before includer), got %v", names)
	}
}

// The including file merges on top of its includes, so a server it redefines
// wins over the same-named server from the include.
func TestLoadHierarchyIncluderOverridesInclude(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "eng", "repos", "myrepo")
	shared := filepath.Join(t.TempDir(), "shared-moxyfile")

	writeConfig(t, shared, `
[[servers]]
name = "grit"
command = "grit shared"
`)
	writeConfig(t, filepath.Join(repoDir, "moxyfile"), `
include = ["`+shared+`"]

[[servers]]
name = "grit"
command = "grit local --verbose"
`)

	result, err := LoadHierarchy(home, repoDir)
	if err != nil {
		t.Fatalf("LoadHierarchy returned error: %v", err)
	}

	if len(result.Merged.Servers) != 1 {
		t.Fatalf("expected 1 server, got %v", serverNames(result.Merged))
	}
	args := result.Merged.Servers[0].Command.Args()
	if len(args) != 2 || args[0] != "local" || args[1] != "--verbose" {
		t.Errorf("expected includer's grit command to win, got %v", args)
	}
}

// A→B→A must be detected and reported, not loop forever.
func TestLoadHierarchyIncludeCycleErrors(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "eng", "repos", "myrepo")
	dir := t.TempDir()
	a := filepath.Join(dir, "a-moxyfile")
	b := filepath.Join(dir, "b-moxyfile")

	writeConfig(t, a, `include = ["`+b+`"]`)
	writeConfig(t, b, `include = ["`+a+`"]`)
	writeConfig(t, filepath.Join(repoDir, "moxyfile"), `include = ["`+a+`"]`)

	_, err := LoadHierarchy(home, repoDir)
	if err == nil {
		t.Fatal("expected an include-cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected error to mention a cycle, got %v", err)
	}
}

// A file including itself is a degenerate cycle.
func TestLoadHierarchyIncludeSelfCycleErrors(t *testing.T) {
	home := t.TempDir()
	repoDir := filepath.Join(home, "eng", "repos", "myrepo")
	self := filepath.Join(t.TempDir(), "self-moxyfile")

	writeConfig(t, self, `include = ["`+self+`"]`)
	writeConfig(t, filepath.Join(repoDir, "moxyfile"), `include = ["`+self+`"]`)

	_, err := LoadHierarchy(home, repoDir)
	if err == nil {
		t.Fatal("expected a self-include cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected error to mention a cycle, got %v", err)
	}
}

func serverNames(c Config) []string {
	names := make([]string, len(c.Servers))
	for i, s := range c.Servers {
		names[i] = s.Name
	}
	return names
}
