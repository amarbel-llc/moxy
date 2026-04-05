package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// readFileWithLineNumbers reads a file and returns its content with line
// numbers. offset is 1-indexed (first line is 1). limit is the max number of
// lines to return (0 = all). Returns the formatted content and total line count.
func readFileWithLineNumbers(path string, offset, limit int) (string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", 0, fmt.Errorf("reading %s: %w", path, err)
	}

	totalLines := len(lines)

	// Apply offset (1-indexed).
	startIdx := 0
	if offset > 0 {
		startIdx = offset - 1
	}
	if startIdx > totalLines {
		startIdx = totalLines
	}

	// Apply limit.
	endIdx := totalLines
	if limit > 0 && startIdx+limit < endIdx {
		endIdx = startIdx + limit
	}

	var b strings.Builder
	for i := startIdx; i < endIdx; i++ {
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, lines[i])
	}

	return b.String(), totalLines, nil
}

// detectBinary checks if a file appears to be binary by scanning the first 8KB
// for null bytes and using http.DetectContentType.
func detectBinary(path string) (bool, string) {
	f, err := os.Open(path)
	if err != nil {
		return false, ""
	}
	defer f.Close()

	buf := make([]byte, 8*1024)
	n, _ := f.Read(buf)
	if n == 0 {
		return false, "text/plain"
	}

	buf = buf[:n]

	// Check for null bytes — strong binary signal.
	if bytes.ContainsRune(buf, 0) {
		mimeType := http.DetectContentType(buf)
		return true, mimeType
	}

	return false, "text/plain"
}

// formatReadSummary formats a progressive disclosure summary for large files.
func formatReadSummary(path string, totalLines int, headContent, tailContent, resourceURI string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "File %s has %d lines (showing head and tail).\n", path, totalLines)
	fmt.Fprintf(&b, "Use %s with ?offset=N&limit=M to read specific sections.\n\n", resourceURI)
	fmt.Fprintf(&b, "--- Head ---\n")
	b.WriteString(headContent)
	fmt.Fprintf(&b, "\n--- Tail ---\n")
	b.WriteString(tailContent)
	return b.String()
}
