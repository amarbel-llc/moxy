package permcheck

import (
	"testing"
)

func TestNewResolver_NoMoxins(t *testing.T) {
	// Use a non-existent MOXIN_PATH (not "") so resolveMoxinDirs skips the
	// CWD-hierarchy fallback that would otherwise pick up ambient
	// .moxy/moxins directories.
	t.Setenv("MOXIN_PATH", t.TempDir()+"/nonexistent")
	r, err := NewResolver()
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	if r == nil {
		t.Fatal("NewResolver returned nil resolver")
	}
}
