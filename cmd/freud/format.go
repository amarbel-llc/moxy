package main

import (
	"fmt"
	"strings"
)

// Column width caps for the columnar format. Real widths are computed from
// the data and clamped to these maxima; longer values are truncated with "…".
const (
	colSessionMax = 30
	colTimeWidth  = 16 // YYYY-MM-DD HH:MM
	colProjectMax = 60
	timeFormat    = "2006-01-02 15:04"
)

// columnWidths holds the per-column character widths used by both the
// header and data rows in a single render call.
type columnWidths struct {
	session  int
	messages int
	size     int
	project  int
}

func computeWidths(rows []sessionRow) columnWidths {
	w := columnWidths{
		session:  len("SESSION"),
		messages: len("MSGS"),
		size:     len("SIZE"),
		project:  len("PROJECT"),
	}
	for _, r := range rows {
		if n := len(r.id); n > w.session {
			w.session = n
		}
		if n := len(fmt.Sprintf("%d", r.messages)); n > w.messages {
			w.messages = n
		}
		if n := len(humanSize(r.size)); n > w.size {
			w.size = n
		}
		if n := len(projectDisplay(r)); n > w.project {
			w.project = n
		}
	}
	if w.session > colSessionMax {
		w.session = colSessionMax
	}
	if w.project > colProjectMax {
		w.project = colProjectMax
	}
	return w
}

// formatSessions renders rows as the Phase 1a columnar format. The header is
// always emitted; an empty rows slice yields the header followed by
// "(no sessions found)" so agents can still see the column meanings.
func formatSessions(rows []sessionRow) string {
	w := computeWidths(rows)

	var b strings.Builder
	b.WriteString(renderHeader(w))
	if len(rows) == 0 {
		b.WriteString("(no sessions found)\n")
		return b.String()
	}
	for _, r := range rows {
		b.WriteString(renderRow(r, w))
		b.WriteByte('\n')
	}
	return b.String()
}

// formatSessionsTruncated emits the head + truncation marker + tail
// representation used when the total row count exceeds max-rows AND no
// pagination params were supplied.
func formatSessionsTruncated(headRows, tailRows []sessionRow, total int, continuationURI string) string {
	all := append(append([]sessionRow{}, headRows...), tailRows...)
	w := computeWidths(all)

	var b strings.Builder
	b.WriteString(renderHeader(w))
	for _, r := range headRows {
		b.WriteString(renderRow(r, w))
		b.WriteByte('\n')
	}
	hidden := total - len(headRows) - len(tailRows)
	fmt.Fprintf(&b, "  ... %d sessions total — %d hidden ...\n", total, hidden)
	for _, r := range tailRows {
		b.WriteString(renderRow(r, w))
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "\nTo retrieve the full list, page through with %s?offset=0&limit=N\n", continuationURI)
	return b.String()
}

func renderHeader(w columnWidths) string {
	return fmt.Sprintf("%-*s  %-*s  %*s  %*s  %s\n",
		w.session, "SESSION",
		colTimeWidth, "LAST ACTIVITY",
		w.messages, "MSGS",
		w.size, "SIZE",
		"PROJECT",
	)
}

func renderRow(r sessionRow, w columnWidths) string {
	return fmt.Sprintf("%-*s  %-*s  %*d  %*s  %s",
		w.session, truncate(r.id, w.session),
		colTimeWidth, r.lastActivity.UTC().Format(timeFormat),
		w.messages, r.messages,
		w.size, humanSize(r.size),
		truncate(projectDisplay(r), w.project),
	)
}

func projectDisplay(r sessionRow) string {
	if r.heuristic {
		return r.projectPath + " (heuristic)"
	}
	return r.projectPath
}

// humanSize renders a byte count with a B/K/M/G suffix, picking the unit
// that yields a 1–3 digit integer where possible.
func humanSize(bytes int64) string {
	const (
		k = 1024
		m = k * 1024
		g = m * 1024
	)
	switch {
	case bytes >= g:
		return fmt.Sprintf("%dG", bytes/g)
	case bytes >= m:
		return fmt.Sprintf("%dM", bytes/m)
	case bytes >= k:
		return fmt.Sprintf("%dK", bytes/k)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// truncate clips s to width bytes, replacing the trailing run with "…" when
// truncation actually occurs.
func truncate(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return s[:width-1] + "…"
}
