package native

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeNamedMoxin(t *testing.T, base, name string) string {
	t.Helper()
	return writeMoxinDir(t, base, name, "schema = 1\nname = \""+name+"\"\n", map[string]string{"status": goodTool})
}

// moxy#101: a MOXIN_PATH entry that is itself a moxin directory serves that
// one moxin; an entry without a _moxin.toml is still scanned for moxin
// subdirectories, as before.
func TestMoxinPathEntryMayBeTheMoxinItself(t *testing.T) {
	soloParent, parent := t.TempDir(), t.TempDir()
	solo := writeNamedMoxin(t, soloParent, "solo")
	// A moxin nested in a moxin directory is not served: the entry is the moxin.
	writeNamedMoxin(t, solo, "inner")
	writeNamedMoxin(t, parent, "a")
	writeNamedMoxin(t, parent, "b")

	result, err := DiscoverAll(solo+string(os.PathListSeparator)+parent, "")
	if err != nil {
		t.Fatalf("DiscoverAll: %v", err)
	}
	names := discoveredNames(result)
	slices.Sort(names)
	if want := []string{"a", "b", "solo"}; !slices.Equal(names, want) {
		t.Fatalf("served moxins = %v, want %v", names, want)
	}
	for _, cfg := range result.Configs {
		if cfg.Name == "solo" && cfg.SourceDir != solo {
			t.Errorf("solo served from %s, want %s", cfg.SourceDir, solo)
		}
	}
}

// A regular file on MOXIN_PATH is not a moxin directory, whatever sits
// beside it: the serve path steps over it as an unreadable entry.
func TestMoxinPathEntryThatIsAFileIsNotAMoxin(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "result")
	if err := os.WriteFile(file, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := DiscoverServing(file, "")
	if err != nil {
		t.Fatalf("DiscoverServing: %v", err)
	}
	if len(result.Configs) != 0 || len(result.Errors) != 0 {
		t.Fatalf("result = %+v, want nothing served and no load errors", result)
	}
}
