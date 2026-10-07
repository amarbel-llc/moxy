package native

import (
	"os"
	"path/filepath"
	"testing"
)

func resolvedDir(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestResolvedMoxinSourcesNamesTheWinningDirectory(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	system := filepath.Join(t.TempDir(), "system")
	writeDiscoveryMoxin(t, dirA, "shared", "from A")
	writeDiscoveryMoxin(t, dirB, "shared", "from B")
	writeDiscoveryMoxin(t, dirB, "only-b", "from B")
	writeDiscoveryMoxin(t, system, "shared", "from system")
	writeDiscoveryMoxin(t, system, "builtin", "from system")

	sources, err := ResolvedMoxinSources(dirA+":"+dirB, t.TempDir(), system)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"shared":  resolvedDir(t, filepath.Join(dirA, "shared")),
		"only-b":  resolvedDir(t, filepath.Join(dirB, "only-b")),
		"builtin": resolvedDir(t, filepath.Join(system, "builtin")),
	}
	if len(sources) != len(want) {
		t.Fatalf("got %v, want %v", sources, want)
	}
	for name, dir := range want {
		if sources[name] != dir {
			t.Errorf("%s: got %q, want %q", name, sources[name], dir)
		}
	}

	// Earlier MOXIN_PATH entries win, so reordering changes who "shared" is.
	reordered, err := ResolvedMoxinSources(dirB+":"+dirA, t.TempDir(), system)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := reordered["shared"], resolvedDir(t, filepath.Join(dirB, "shared")); got != want {
		t.Errorf("reordered: got %q, want %q", got, want)
	}
}

func TestResolvedMoxinSourcesSeesThroughSymlinks(t *testing.T) {
	real := filepath.Join(t.TempDir(), "real")
	writeDiscoveryMoxin(t, real, "alpha", "the one moxin")
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	viaReal, err := ResolvedMoxinSources(real, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	viaLink, err := ResolvedMoxinSources(link, t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if viaReal["alpha"] == "" || viaReal["alpha"] != viaLink["alpha"] {
		t.Errorf("two spellings of one directory must compare equal: %q vs %q", viaReal["alpha"], viaLink["alpha"])
	}
}

func TestResolvedMoxinSourcesUsesTheGivenDirectoryForTheDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, "project")
	elsewhere := filepath.Join(home, "elsewhere")
	writeDiscoveryMoxin(t, filepath.Join(project, ".moxy", "moxins"), "local", "project-local")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(elsewhere)

	sources, err := ResolvedMoxinSources("", project, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sources["local"]; !ok {
		t.Errorf("the default MOXIN_PATH must be computed from the given directory, not the working one: %v", sources)
	}
}
