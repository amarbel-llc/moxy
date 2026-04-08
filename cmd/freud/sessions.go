package main

import (
	"bufio"
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

// scanProjectSessions returns sessions filtered to a single project. The
// projectKey is matched first against raw directory names, then against
// resolved cwd absolute paths. Returns nil with no error when no project
// matches; the caller is responsible for turning that into a structured
// "unknown project" hint.
func scanProjectSessions(projectsDir string, cache *projectCache, projectKey string) ([]sessionRow, []projectInfo, error) {
	all, err := scanAllSessions(projectsDir, cache)
	if err != nil {
		return nil, nil, err
	}

	projects, err := cache.scanProjects(projectsDir)
	if err != nil {
		return nil, nil, err
	}

	var matchedDir string
	for _, p := range projects {
		if p.dirName == projectKey {
			matchedDir = p.dirName
			break
		}
	}
	if matchedDir == "" {
		for _, p := range projects {
			if p.absPath != "" && p.absPath == projectKey {
				matchedDir = p.dirName
				break
			}
		}
	}
	if matchedDir == "" {
		return nil, projects, nil
	}

	var filtered []sessionRow
	for _, r := range all {
		if r.projectDir == matchedDir {
			filtered = append(filtered, r)
		}
	}
	return filtered, projects, nil
}

// pageRows applies offset/limit pagination to an already-sorted row slice.
// A zero limit means "all rows from offset to end".
func pageRows(rows []sessionRow, offset, limit int) []sessionRow {
	if offset >= len(rows) {
		return nil
	}
	end := len(rows)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return rows[offset:end]
}
