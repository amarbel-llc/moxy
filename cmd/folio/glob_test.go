package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobFiles_SimplePattern(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("b"), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c"), 0o644)

	matches, err := globFiles("*.go", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}
	for _, m := range matches {
		if filepath.Ext(m) != ".go" {
			t.Fatalf("unexpected match: %s", m)
		}
	}
}

func TestGlobFiles_DoubleStarPattern(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src", "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "top.go"), []byte("t"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("m"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "util.go"), []byte("u"), 0o644)
	os.WriteFile(filepath.Join(dir, "src", "pkg", "data.txt"), []byte("d"), 0o644)

	matches, err := globFiles("**/*.go", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 3 {
		t.Fatalf("expected 3 matches, got %d: %v", len(matches), matches)
	}
}

func TestGlobFiles_NoMatches(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)

	matches, err := globFiles("*.go", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestGlobFiles_SubdirPattern(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("m"), 0o644)
	os.WriteFile(filepath.Join(dir, "top.go"), []byte("t"), 0o644)

	matches, err := globFiles("src/*.go", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(matches), matches)
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"*.go", "main.go", true},
		{"*.go", "main.txt", false},
		{"**/*.go", "main.go", true},
		{"**/*.go", "src/main.go", true},
		{"**/*.go", "src/pkg/main.go", true},
		{"src/**", "src/main.go", true},
		{"src/**", "src/pkg/main.go", true},
		{"src/*.go", "src/main.go", true},
		{"src/*.go", "src/pkg/main.go", false},
	}

	for _, tt := range tests {
		pattern := splitPath(tt.pattern)
		path := splitPath(tt.path)
		got := matchGlob(pattern, path)
		if got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func splitPath(s string) []string {
	return strings.Split(filepath.ToSlash(s), "/")
}
