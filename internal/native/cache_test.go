package native

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

func TestResultCacheStoreAndLoad(t *testing.T) {
	dir := t.TempDir()
	cache := newResultCache(dir)

	original := cachedResult{
		ID:         "01964abc-def0-7000-8000-000000000001",
		Session:    "test-session",
		Output:     "total 48\ndrwxr-xr-x 5 user staff 160\n",
		LineCount:  2,
		TokenCount: 12,
	}

	if err := cache.store(original); err != nil {
		t.Fatalf("store: %v", err)
	}

	// Verify files exist under the session subdirectory.
	sessionDir := filepath.Join(dir, original.Session)
	if _, err := os.Stat(filepath.Join(sessionDir, original.ID+".txt")); err != nil {
		t.Fatalf("output file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, original.ID+".json")); err != nil {
		t.Fatalf("metadata file missing: %v", err)
	}

	loaded, err := cache.load(original.Session, original.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.ID != original.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, original.ID)
	}
	if loaded.Session != original.Session {
		t.Errorf("Session = %q, want %q", loaded.Session, original.Session)
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

func TestResultCacheLoadMissing(t *testing.T) {
	dir := t.TempDir()
	cache := newResultCache(dir)

	_, err := cache.load("test-session", "nonexistent")
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

	result := cachedResult{
		ID:         "01964abc-def0-7000-8000-000000000002",
		Session:    "sess-x",
		Output:     output,
		LineCount:  30,
		TokenCount: 82,
	}

	summary := formatSummary(result)

	if !strings.Contains(summary, "Lines: 30") {
		t.Error("summary missing line count")
	}
	if strings.Contains(summary, "Tokens") {
		t.Error("summary should not contain token count")
	}
	if !strings.Contains(summary, "First 10 lines") {
		t.Error("summary missing head section")
	}
	if !strings.Contains(summary, "Last 10 lines") {
		t.Error("summary missing tail section")
	}
	wantURI := "moxy.native://results/" + result.Session + "/" + result.ID
	if !strings.Contains(summary, wantURI) {
		t.Errorf("summary missing resource URI %q", wantURI)
	}
	// New: truncation warning at top.
	if !strings.Contains(summary, "⚠ TRUNCATED") {
		t.Error("summary missing leading truncation warning")
	}
	if !strings.Contains(summary, "showing 20 of 30 lines") {
		t.Error("summary missing line count in truncation warning")
	}
	// New: gap marker between head and tail.
	if !strings.Contains(summary, "10 lines omitted (11 through 20)") {
		t.Error("summary missing gap marker between head and tail")
	}
	// New: trailing warning.
	if !strings.Contains(summary, "Read full output URI before drawing conclusions") {
		t.Error("summary missing trailing warning")
	}
}

func TestFormatSummaryShortOutput(t *testing.T) {
	// Output with fewer lines than head+tail should not duplicate.
	output := "line1\nline2\nline3\n"

	result := cachedResult{
		ID:         "01964abc-def0-7000-8000-000000000003",
		Session:    "sess-y",
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

func TestFormatSummarySingleLineLargeOutput(t *testing.T) {
	// A single line exceeding summaryMaxOutputBytes must be truncated.
	bigLine := strings.Repeat("x", 5000)
	result := cachedResult{
		ID:         "01964abc-def0-7000-8000-000000000010",
		Session:    "sess-big",
		Output:     bigLine,
		LineCount:  1,
		TokenCount: 1250,
	}

	summary := formatSummary(result)

	if !strings.Contains(summary, "Output (truncated)") {
		t.Error("large single-line output should have truncated header")
	}
	if !strings.Contains(summary, "5000 total characters") {
		t.Errorf("summary should report total character count, got:\n%s", summary)
	}
	if len(summary) > summaryMaxOutputBytes+500 {
		t.Errorf("summary too large: %d bytes (max output %d + overhead)", len(summary), summaryMaxOutputBytes)
	}
	wantURI := "moxy.native://results/" + result.Session + "/" + result.ID
	if !strings.Contains(summary, wantURI) {
		t.Errorf("summary missing resource URI %q", wantURI)
	}
}

func TestFormatSummaryLongLinesInHeadTail(t *testing.T) {
	// 30 lines where each line is very long — head/tail sections should truncate.
	longLine := strings.Repeat("y", 500)
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, longLine)
	}
	output := strings.Join(lines, "\n") + "\n"

	result := cachedResult{
		ID:         "01964abc-def0-7000-8000-000000000011",
		Session:    "sess-long",
		Output:     output,
		LineCount:  30,
		TokenCount: 3750,
	}

	summary := formatSummary(result)

	if !strings.Contains(summary, "First 10 lines") {
		t.Error("summary missing head section")
	}
	if !strings.Contains(summary, "Last 10 lines") {
		t.Error("summary missing tail section")
	}
	if !strings.Contains(summary, "(truncated)") {
		t.Error("sections with long lines should be truncated")
	}
	if !strings.Contains(summary, "total characters in section") {
		t.Error("truncated sections should report character count")
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single line no newline", "hello", 1},
		{"single line with newline", "hello\n", 1},
		{"two lines", "a\nb\n", 2},
		{"no trailing newline", "a\nb", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countLines(tt.input)
			if got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseResultURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantSession string
		wantID      string
		wantOK      bool
	}{
		{"valid", "moxy.native://results/sess1/abc-123", "sess1", "abc-123", true},
		{"valid with dots", "moxy.native://results/sess.1_a/abc-123", "sess.1_a", "abc-123", true},
		{"with query", "moxy.native://results/sess1/abc-123?foo=bar", "sess1", "abc-123", true},
		{"wrong scheme", "man://results/sess1/abc-123", "", "", false},
		{"maneater scheme", "maneater.exec://results/sess1/abc-123", "", "", false},
		{"missing id segment", "moxy.native://results/sess1", "", "", false},
		{"trailing slash", "moxy.native://results/sess1/", "", "", false},
		{"empty session", "moxy.native://results//abc-123", "", "", false},
		{"no segments", "moxy.native://results/", "", "", false},
		{"no prefix", "something-else", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, id, ok := parseResultURI(tt.uri)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if session != tt.wantSession {
				t.Errorf("session = %q, want %q", session, tt.wantSession)
			}
			if id != tt.wantID {
				t.Errorf("id = %q, want %q", id, tt.wantID)
			}
		})
	}
}
