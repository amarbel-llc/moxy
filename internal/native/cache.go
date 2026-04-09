package native

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	resultTokenThreshold = 50
	tokenThreshold       = resultTokenThreshold
	summaryHeadLines     = 10
	summaryTailLines     = 10
)

// resultCache stores tool outputs on disk, keyed by session and id.
type resultCache struct {
	dir string
}

type cachedResult struct {
	ID         string `json:"id"`
	Session    string `json:"session"`
	Command    string `json:"command"`
	Output     string `json:"-"`
	LineCount  int    `json:"line_count"`
	TokenCount int    `json:"token_count"`
}

func newResultCache(dir string) *resultCache {
	if dir == "" {
		if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
			dir = filepath.Join(xdg, "moxy", "native-results")
		} else {
			home, _ := os.UserHomeDir()
			dir = filepath.Join(home, ".cache", "moxy", "native-results")
		}
	}
	return &resultCache{dir: dir}
}

func (c *resultCache) store(result cachedResult) error {
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

func (c *resultCache) load(session, id string) (*cachedResult, error) {
	sessionDir := filepath.Join(c.dir, session)

	metaPath := filepath.Join(sessionDir, id+".json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("reading metadata: %w", err)
	}

	var result cachedResult
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

func formatSummary(result cachedResult) string {
	lines := strings.Split(result.Output, "\n")
	// Remove trailing empty line from trailing newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Command: %s\n", result.Command)
	fmt.Fprintf(&b, "Lines: %d | Tokens: ~%d (exceeds threshold of %d)\n",
		result.LineCount, result.TokenCount, resultTokenThreshold)
	b.WriteString("\n")

	totalLines := len(lines)
	head := summaryHeadLines
	tail := summaryTailLines

	if totalLines <= head+tail {
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

	fmt.Fprintf(&b, "\nFull output: moxy.native://results/%s/%s", result.Session, result.ID)
	return b.String()
}

func newResultID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
