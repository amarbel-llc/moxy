package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"short", "hi", 1},
		{"exact boundary", "1234", 1},
		{"typical line", "total 48K\ndrwxr-xr-x 5 user staff 160 Apr  5 10:00 .\n", 13},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateTokens(tt.input)
			if got != tt.want {
				t.Errorf("estimateTokens(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestExecResultCacheStoreAndLoad(t *testing.T) {
	dir := t.TempDir()
	cache := &execResultCache{dir: dir}

	original := cachedExecResult{
		ID:         "01964abc-def0-7000-8000-000000000001",
		Command:    "ls -la",
		Output:     "total 48\ndrwxr-xr-x 5 user staff 160\n",
		LineCount:  2,
		TokenCount: 12,
	}

	if err := cache.store(original); err != nil {
		t.Fatalf("store: %v", err)
	}

	// Verify files exist.
	if _, err := os.Stat(filepath.Join(dir, original.ID+".txt")); err != nil {
		t.Fatalf("output file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, original.ID+".json")); err != nil {
		t.Fatalf("metadata file missing: %v", err)
	}

	loaded, err := cache.load(original.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.ID != original.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, original.ID)
	}
	if loaded.Command != original.Command {
		t.Errorf("Command = %q, want %q", loaded.Command, original.Command)
	}
	if loaded.Output != original.Output {
		t.Errorf("Output = %q, want %q", loaded.Output, original.Output)
	}
	if loaded.LineCount != original.LineCount {
		t.Errorf("LineCount = %d, want %d", loaded.LineCount, original.LineCount)
	}
	if loaded.TokenCount != original.TokenCount {
		t.Errorf("TokenCount = %d, want %d", loaded.TokenCount, original.TokenCount)
	}
}

func TestExecResultCacheLoadMissing(t *testing.T) {
	dir := t.TempDir()
	cache := &execResultCache{dir: dir}

	_, err := cache.load("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing ID")
	}
}

func TestFormatSummary(t *testing.T) {
	// Generate 30 lines of output.
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, strings.Repeat("x", 10))
	}
	output := strings.Join(lines, "\n") + "\n"

	result := cachedExecResult{
		ID:         "01964abc-def0-7000-8000-000000000002",
		Command:    "generate-stuff",
		Output:     output,
		LineCount:  30,
		TokenCount: 82,
	}

	summary := formatSummary(result)

	if !strings.Contains(summary, "Command: generate-stuff") {
		t.Error("summary missing command")
	}
	if !strings.Contains(summary, "Lines: 30") {
		t.Error("summary missing line count")
	}
	if !strings.Contains(summary, "Tokens: ~82") {
		t.Error("summary missing token count")
	}
	if !strings.Contains(summary, "First 10 lines") {
		t.Error("summary missing head section")
	}
	if !strings.Contains(summary, "Last 10 lines") {
		t.Error("summary missing tail section")
	}
	if !strings.Contains(summary, "maneater.exec://results/"+result.ID) {
		t.Error("summary missing resource URI")
	}
}

func TestFormatSummaryShortOutput(t *testing.T) {
	// Output with fewer lines than head+tail should not duplicate.
	output := "line1\nline2\nline3\n"

	result := cachedExecResult{
		ID:         "01964abc-def0-7000-8000-000000000003",
		Command:    "echo short",
		Output:     output,
		LineCount:  3,
		TokenCount: 5,
	}

	summary := formatSummary(result)

	if strings.Contains(summary, "First") || strings.Contains(summary, "Last") {
		t.Error("short output should use single Output section, not First/Last")
	}
	if !strings.Contains(summary, "--- Output ---") {
		t.Error("short output should have Output header")
	}
	// Each line should appear exactly once.
	if strings.Count(summary, "line1") != 1 {
		t.Error("line1 appears more than once")
	}
}

func TestParseExecResultURI(t *testing.T) {
	tests := []struct {
		name   string
		uri    string
		wantID string
		wantOK bool
	}{
		{"valid", "maneater.exec://results/abc-123", "abc-123", true},
		{"with query", "maneater.exec://results/abc-123?foo=bar", "abc-123", true},
		{"wrong scheme", "man://results/abc-123", "", false},
		{"empty id", "maneater.exec://results/", "", false},
		{"no prefix", "something-else", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := parseExecResultURI(tt.uri)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if id != tt.wantID {
				t.Errorf("id = %q, want %q", id, tt.wantID)
			}
		})
	}
}
