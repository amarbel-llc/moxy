package native

import (
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

func TestFormatSummary(t *testing.T) {
	// Generate 30 lines of output.
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, strings.Repeat("x", 10))
	}
	output := strings.Join(lines, "\n") + "\n"
	digest := "blake2b256-aaaaaaaaaaaaaaaaaaaaaaaa"

	summary := formatSummary(output, digest)

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
	wantURI := "madder://blobs/" + digest
	if !strings.Contains(summary, wantURI) {
		t.Errorf("summary missing resource URI %q", wantURI)
	}
	if !strings.Contains(summary, "⚠ TRUNCATED") {
		t.Error("summary missing leading truncation warning")
	}
	if !strings.Contains(summary, "showing 20 of 30 lines") {
		t.Error("summary missing line count in truncation warning")
	}
	if !strings.Contains(summary, "10 lines omitted (11 through 20)") {
		t.Error("summary missing gap marker between head and tail")
	}
	if !strings.Contains(summary, "Read full output URI before drawing conclusions") {
		t.Error("summary missing trailing warning")
	}
}

func TestFormatSummaryShortOutput(t *testing.T) {
	output := "line1\nline2\nline3\n"
	summary := formatSummary(output, "blake2b256-bbbb")

	if strings.Contains(summary, "First") || strings.Contains(summary, "Last") {
		t.Error("short output should use single Output section, not First/Last")
	}
	if !strings.Contains(summary, "--- Output ---") {
		t.Error("short output should have Output header")
	}
	if strings.Count(summary, "line1") != 1 {
		t.Error("line1 appears more than once")
	}
}

func TestFormatSummarySingleLineLargeOutput(t *testing.T) {
	bigLine := strings.Repeat("x", 5000)
	digest := "blake2b256-cccc"
	summary := formatSummary(bigLine, digest)

	if !strings.Contains(summary, "Output (truncated)") {
		t.Error("large single-line output should have truncated header")
	}
	if !strings.Contains(summary, "5000 total characters") {
		t.Errorf("summary should report total character count, got:\n%s", summary)
	}
	if len(summary) > summaryMaxOutputBytes+500 {
		t.Errorf("summary too large: %d bytes (max output %d + overhead)", len(summary), summaryMaxOutputBytes)
	}
	wantURI := "madder://blobs/" + digest
	if !strings.Contains(summary, wantURI) {
		t.Errorf("summary missing resource URI %q", wantURI)
	}
}

func TestFormatSummaryLongLinesInHeadTail(t *testing.T) {
	longLine := strings.Repeat("y", 500)
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, longLine)
	}
	output := strings.Join(lines, "\n") + "\n"
	summary := formatSummary(output, "blake2b256-dddd")

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

func TestParseBlobURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantDigest string
		wantOK     bool
	}{
		{"valid blake2b256", "madder://blobs/blake2b256-abc123_def", "blake2b256-abc123_def", true},
		{"with query", "madder://blobs/blake2b256-abc?foo=bar", "blake2b256-abc", true},
		{"wrong scheme", "moxy.native://results/sess/abc", "", false},
		{"missing digest", "madder://blobs/", "", false},
		{"trailing slash", "madder://blobs/abc/", "", false},
		{"no prefix", "something-else", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			digest, ok := parseBlobURI(tt.uri)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if digest != tt.wantDigest {
				t.Errorf("digest = %q, want %q", digest, tt.wantDigest)
			}
		})
	}
}
