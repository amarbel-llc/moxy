package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileWithLineNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("line one\nline two\nline three\n"), 0o644)

	content, total, err := readFileWithLineNumbers(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("expected 3 lines, got %d", total)
	}
	if !strings.Contains(content, "     1\tline one") {
		t.Fatalf("expected line numbers, got:\n%s", content)
	}
	if !strings.Contains(content, "     3\tline three") {
		t.Fatalf("expected line 3, got:\n%s", content)
	}
}

func TestReadFileWithOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0o644)

	content, total, err := readFileWithLineNumbers(path, 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("expected 5 total, got %d", total)
	}
	// Should contain lines 3 and 4.
	if !strings.Contains(content, "     3\tc") {
		t.Fatalf("expected line 3, got:\n%s", content)
	}
	if !strings.Contains(content, "     4\td") {
		t.Fatalf("expected line 4, got:\n%s", content)
	}
	// Should not contain line 5.
	if strings.Contains(content, "     5\te") {
		t.Fatalf("line 5 should not appear, got:\n%s", content)
	}
}

func TestReadFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte{}, 0o644)

	content, total, err := readFileWithLineNumbers(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("expected 0 lines, got %d", total)
	}
	if content != "" {
		t.Fatalf("expected empty, got:\n%s", content)
	}
}

func TestReadFileNotFound(t *testing.T) {
	_, _, err := readFileWithLineNumbers("/nonexistent/path", 0, 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestDetectBinary(t *testing.T) {
	dir := t.TempDir()

	// Text file.
	textPath := filepath.Join(dir, "text.txt")
	os.WriteFile(textPath, []byte("hello world\n"), 0o644)
	isBin, _ := detectBinary(textPath)
	if isBin {
		t.Fatal("text file detected as binary")
	}

	// Binary file (contains null bytes).
	binPath := filepath.Join(dir, "binary.bin")
	os.WriteFile(binPath, []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x00}, 0o644)
	isBin, mime := detectBinary(binPath)
	if !isBin {
		t.Fatal("binary file not detected")
	}
	if mime == "" {
		t.Fatal("expected MIME type for binary")
	}
}

func TestFormatReadSummary(t *testing.T) {
	summary := formatReadSummary("/path/to/file.go", 5000, "head content", "tail content", "folio://read//path/to/file.go")
	if !strings.Contains(summary, "5000 lines") {
		t.Fatalf("expected line count in summary, got:\n%s", summary)
	}
	if !strings.Contains(summary, "head content") {
		t.Fatalf("expected head content, got:\n%s", summary)
	}
	if !strings.Contains(summary, "tail content") {
		t.Fatalf("expected tail content, got:\n%s", summary)
	}
}
