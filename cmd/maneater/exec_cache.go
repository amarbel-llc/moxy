package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// TODO(calibrate): tune threshold and estimator based on real-world usage.
// Current heuristic is chars/4 which is imprecise (overcounts code/URLs,
// undercounts CJK). A tiktoken-based counter would be more accurate but
// adds a dependency. No universal token counter exists since models tokenize
// differently, but tiktoken is a reasonable proxy for threshold purposes.
// At chars/4, 50 tokens ≈ 200 characters ≈ 3-4 lines of typical command output.
const execTokenThreshold = 50

const (
	summaryHeadLines = 10
	summaryTailLines = 10
)

type execResultCache struct {
	dir string
}

type cachedExecResult struct {
	ID         string `json:"id"`
	Session    string `json:"session"`
	Command    string `json:"command"`
	Output     string `json:"-"`
	LineCount  int    `json:"line_count"`
	TokenCount int    `json:"token_count"`
}

func newExecResultCache() *execResultCache {
	var base string
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		base = filepath.Join(xdg, "maneater", "exec-results")
	} else {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache", "maneater", "exec-results")
	}
	return &execResultCache{dir: base}
}

func (c *execResultCache) store(result cachedExecResult) error {
	if result.Session == "" {
		return fmt.Errorf("store: missing session")
	}
	sessionDir := filepath.Join(c.dir, result.Session)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	outputPath := filepath.Join(sessionDir, result.ID+".txt")
	if err := os.WriteFile(outputPath, []byte(result.Output), 0o644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	metaPath := filepath.Join(sessionDir, result.ID+".json")
	meta, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshaling metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, meta, 0o644); err != nil {
		return fmt.Errorf("writing metadata: %w", err)
	}

	return nil
}

func (c *execResultCache) load(session, id string) (*cachedExecResult, error) {
	sessionDir := filepath.Join(c.dir, session)
	metaPath := filepath.Join(sessionDir, id+".json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("reading metadata: %w", err)
	}

	var result cachedExecResult
	if err := json.Unmarshal(metaData, &result); err != nil {
		return nil, fmt.Errorf("parsing metadata: %w", err)
	}

	outputPath := filepath.Join(sessionDir, id+".txt")
	output, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("reading output: %w", err)
	}
	result.Output = string(output)

	return &result, nil
}

func estimateTokens(text string) int {
	n := len(text) / 4
	if n == 0 && len(text) > 0 {
		return 1
	}
	return n
}

func formatSummary(result cachedExecResult) string {
	lines := strings.Split(result.Output, "\n")
	// Remove trailing empty line from trailing newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Command: %s\n", result.Command)
	fmt.Fprintf(&b, "Lines: %d | Tokens: ~%d (exceeds threshold of %d)\n",
		result.LineCount, result.TokenCount, execTokenThreshold)
	b.WriteString("\n")

	totalLines := len(lines)
	head := summaryHeadLines
	tail := summaryTailLines

	if totalLines <= head+tail {
		// Output fits in head+tail — show it all, no duplication.
		b.WriteString("--- Output ---\n")
		for _, line := range lines {
			b.WriteString(line)
			b.WriteString("\n")
		}
	} else {
		fmt.Fprintf(&b, "--- First %d lines ---\n", head)
		for _, line := range lines[:head] {
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "--- Last %d lines ---\n", tail)
		for _, line := range lines[totalLines-tail:] {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	fmt.Fprintf(&b, "\nFull output: maneater.exec://results/%s/%s", result.Session, result.ID)
	return b.String()
}

func newExecResultID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// parseExecResultURI extracts the session and id segments from a
// maneater.exec://results/{session}/{id} URI. Returns ok=false for any URI
// that does not match the two-segment form.
func parseExecResultURI(uri string) (session, id string, ok bool) {
	const prefix = "maneater.exec://results/"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", false
	}
	rest := uri[len(prefix):]
	if idx := strings.Index(rest, "?"); idx >= 0 {
		rest = rest[:idx]
	}
	slash := strings.Index(rest, "/")
	if slash <= 0 || slash == len(rest)-1 {
		return "", "", false
	}
	return rest[:slash], rest[slash+1:], true
}
