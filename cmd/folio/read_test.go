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

func TestParseReadURI(t *testing.T) {
	cases := []struct {
		name            string
		uri             string
		wantPath        string
		wantOffset      int
		wantLimit       int
		wantDeleteStart int
		wantDeleteEnd   int
		wantErr         bool
	}{
		// ?offset=N&end=M should behave like sed -n 'N,Mp': inclusive range.
		{
			name:       "offset and end",
			uri:        "folio://read//tmp/x.go?offset=369&end=380",
			wantPath:   "/tmp/x.go",
			wantOffset: 369,
			wantLimit:  12,
		},
		{
			name:       "end without offset defaults start to 1",
			uri:        "folio://read//tmp/x.go?end=10",
			wantPath:   "/tmp/x.go",
			wantOffset: 1,
			wantLimit:  10,
		},
		{
			name:       "end equal to offset returns single line",
			uri:        "folio://read//tmp/x.go?offset=5&end=5",
			wantPath:   "/tmp/x.go",
			wantOffset: 5,
			wantLimit:  1,
		},
		{
			name:    "end less than offset is an error",
			uri:     "folio://read//tmp/x.go?offset=10&end=5",
			wantErr: true,
		},
		{
			name:    "non-numeric end is an error",
			uri:     "folio://read//tmp/x.go?end=abc",
			wantErr: true,
		},
		// ?delete=N-M is sed 'N,Md': omit an inclusive range.
		{
			name:            "delete range",
			uri:             "folio://read//tmp/x.go?delete=5-10",
			wantPath:        "/tmp/x.go",
			wantDeleteStart: 5,
			wantDeleteEnd:   10,
		},
		{
			name:            "delete single line",
			uri:             "folio://read//tmp/x.go?delete=7-7",
			wantPath:        "/tmp/x.go",
			wantDeleteStart: 7,
			wantDeleteEnd:   7,
		},
		{
			name:    "delete end less than start is an error",
			uri:     "folio://read//tmp/x.go?delete=10-5",
			wantErr: true,
		},
		{
			name:    "delete without dash is an error",
			uri:     "folio://read//tmp/x.go?delete=5",
			wantErr: true,
		},
		{
			name:    "delete start below 1 is an error",
			uri:     "folio://read//tmp/x.go?delete=0-5",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := parseReadURI(tc.uri)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", p)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.path != tc.wantPath {
				t.Errorf("path: want %q got %q", tc.wantPath, p.path)
			}
			if p.offset != tc.wantOffset {
				t.Errorf("offset: want %d got %d", tc.wantOffset, p.offset)
			}
			if p.limit != tc.wantLimit {
				t.Errorf("limit: want %d got %d", tc.wantLimit, p.limit)
			}
			if p.deleteStart != tc.wantDeleteStart {
				t.Errorf("deleteStart: want %d got %d", tc.wantDeleteStart, p.deleteStart)
			}
			if p.deleteEnd != tc.wantDeleteEnd {
				t.Errorf("deleteEnd: want %d got %d", tc.wantDeleteEnd, p.deleteEnd)
			}
		})
	}
}

func TestReadFileFilteredExcludeRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("a\nb\nc\nd\ne\n"), 0o644)

	// sed '2,4d' — drop lines 2, 3, 4, keep 1 and 5.
	content, total, err := readFileFiltered(path, 0, 0, 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("expected 5 total, got %d", total)
	}
	if !strings.Contains(content, "     1\ta") {
		t.Errorf("expected line 1, got:\n%s", content)
	}
	if !strings.Contains(content, "     5\te") {
		t.Errorf("expected line 5, got:\n%s", content)
	}
	for _, bad := range []string{"     2\tb", "     3\tc", "     4\td"} {
		if strings.Contains(content, bad) {
			t.Errorf("line %q should have been excluded, got:\n%s", bad, content)
		}
	}
}

func TestFormatReadToolOutput(t *testing.T) {
	out := formatReadToolOutput("folio://read//tmp/x.go?offset=1&end=2", "     1\ta\n     2\tb\n")
	lines := strings.SplitN(out, "\n", 2)
	if lines[0] != "folio://read//tmp/x.go?offset=1&end=2" {
		t.Errorf("first line should be URI, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "     1\ta") || !strings.Contains(lines[1], "     2\tb") {
		t.Errorf("expected line-numbered content after URI, got:\n%s", lines[1])
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
