package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// sessionRow holds the per-session metadata used by both the resource handler
// and the future columnar formatter (commit 3).
type sessionRow struct {
	id           string
	projectDir   string    // raw directory name on disk
	projectPath  string    // resolved cwd, or heuristic decode
	heuristic    bool      // true when projectPath came from name decoding
	lastActivity time.Time // mtime of the JSONL
	messages     int       // line count
	size         int64     // bytes
}

// scanAllSessions enumerates every JSONL file under projectsDir, sorted by
// last activity (newest first). Project path resolution is delegated to the
// shared projectCache.
func scanAllSessions(projectsDir string, cache *projectCache) ([]sessionRow, error) {
	projects, err := cache.scanProjects(projectsDir)
	if err != nil {
		return nil, err
	}

	var rows []sessionRow
	for _, p := range projects {
		dirPath := filepath.Join(projectsDir, p.dirName)
		entries, readErr := os.ReadDir(dirPath)
		if readErr != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			full := filepath.Join(dirPath, entry.Name())
			stat, statErr := os.Stat(full)
			if statErr != nil {
				continue
			}
			rows = append(rows, sessionRow{
				id:           strings.TrimSuffix(entry.Name(), ".jsonl"),
				projectDir:   p.dirName,
				projectPath:  p.absPath,
				heuristic:    p.heuristic,
				lastActivity: stat.ModTime(),
				messages:     countLines(full),
				size:         stat.Size(),
			})
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].lastActivity.After(rows[j].lastActivity)
	})

	return rows, nil
}

// countLines returns the number of newline-terminated lines in path.
// Errors yield 0 — message count is informational, not load-bearing.
func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count
}

// formatSessionsMinimal produces the Phase 1a (commit 2) listing: one row
// per session, tab-separated, with the columns the bats tests need to find.
// Commit 3 will replace this with a fixed-width columnar formatter.
func formatSessionsMinimal(rows []sessionRow) string {
	if len(rows) == 0 {
		return "(no sessions found)\n"
	}
	var b strings.Builder
	for _, r := range rows {
		project := r.projectPath
		if r.heuristic {
			project += " (heuristic)"
		}
		fmt.Fprintf(&b, "%s\t%s\t%d\t%d\t%s\n",
			r.id,
			r.lastActivity.UTC().Format(time.RFC3339),
			r.messages,
			r.size,
			project,
		)
	}
	return b.String()
}
