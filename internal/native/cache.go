package native

import (
	"fmt"
	"strings"
)

const (
	resultTokenThreshold  = 200
	tokenThreshold        = resultTokenThreshold
	summaryHeadLines      = 10
	summaryTailLines      = 10
	summaryMaxOutputBytes = 2000
)

// blobURI builds the canonical moxy form of a madder blob reference.
func blobURI(digest string) string {
	return "madder://blobs/" + digest
}

func countLines(text string) int {
	if text == "" {
		return 0
	}
	n := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		n++
	}
	return n
}

func estimateTokens(text string) int {
	n := len(text) / 4
	if n == 0 && len(text) > 0 {
		return 1
	}
	return n
}

// formatSummary renders the head/tail summary returned to the MCP client
// for cached outputs. The full bytes live in madder under `digest`; the
// summary points at madder://blobs/<digest> and tells the reader how to
// retrieve them.
func formatSummary(output, digest string) string {
	lines := strings.Split(output, "\n")
	// Remove trailing empty line from trailing newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var b strings.Builder

	totalLines := len(lines)
	head := summaryHeadLines
	tail := summaryTailLines
	showingHeadTail := totalLines > head+tail

	if showingHeadTail {
		shown := head + tail
		fmt.Fprintf(&b, "⚠ TRUNCATED — showing %d of %d lines. Full output below.\n", shown, totalLines)
	}
	fmt.Fprintf(&b, "Full output: %s\n", blobURI(digest))
	fmt.Fprintf(&b, "Lines: %d\n", countLines(output))
	b.WriteString("\n")

	if !showingHeadTail {
		content := strings.Join(lines, "\n")
		if len(content) > summaryMaxOutputBytes {
			b.WriteString("--- Output (truncated) ---\n")
			b.WriteString(content[:summaryMaxOutputBytes])
			fmt.Fprintf(&b, "\n... (%d total characters)\n", len(output))
		} else {
			b.WriteString("--- Output ---\n")
			for _, line := range lines {
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
	} else {
		writeSection(&b, fmt.Sprintf("--- First %d lines ---", head), lines[:head])
		omitted := totalLines - head - tail
		fmt.Fprintf(&b, "\n--- %d lines omitted (%d through %d) ---\n\n", omitted, head+1, totalLines-tail)
		writeSection(&b, fmt.Sprintf("--- Last %d lines ---", tail), lines[totalLines-tail:])
		fmt.Fprintf(&b, "\n⚠ %d lines were omitted. Read full output URI before drawing conclusions.\n", omitted)
	}
	return b.String()
}

func writeSection(b *strings.Builder, header string, lines []string) {
	content := strings.Join(lines, "\n")
	if len(content) > summaryMaxOutputBytes {
		b.WriteString(header + " (truncated)\n")
		b.WriteString(content[:summaryMaxOutputBytes])
		fmt.Fprintf(b, "\n... (%d total characters in section)\n", len(content))
	} else {
		b.WriteString(header + "\n")
		for _, line := range lines {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
}
