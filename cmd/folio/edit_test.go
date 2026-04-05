package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditFile_SingleMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0o644)

	count, err := editFile(path, "world", "universe", false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 replacement, got %d", count)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello universe" {
		t.Fatalf("expected 'hello universe', got %q", data)
	}
}

func TestEditFile_NoMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0o644)

	_, err := editFile(path, "missing", "replacement", false)
	if err == nil {
		t.Fatal("expected error for no match")
	}
	if !strings.Contains(err.Error(), "no match found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEditFile_AmbiguousMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("foo bar foo baz foo"), 0o644)

	_, err := editFile(path, "foo", "qux", false)
	if err == nil {
		t.Fatal("expected error for ambiguous match")
	}
	if !strings.Contains(err.Error(), "3 matches") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEditFile_ReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("foo bar foo baz foo"), 0o644)

	count, err := editFile(path, "foo", "qux", true)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 replacements, got %d", count)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "qux bar qux baz qux" {
		t.Fatalf("expected all replaced, got %q", data)
	}
}

func TestEditFile_MultiLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("line one\nline two\nline three\n"), 0o644)

	count, err := editFile(path, "line two\nline three", "replaced", false)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 replacement, got %d", count)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "line one\nreplaced\n" {
		t.Fatalf("unexpected content: %q", data)
	}
}
