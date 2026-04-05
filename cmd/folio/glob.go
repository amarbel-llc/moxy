package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// globFiles finds files matching a glob pattern rooted at dir. Supports **
// for recursive matching. Returns paths sorted by modification time (newest
// first).
func globFiles(pattern, dir string) ([]string, error) {
	if dir == "" {
		dir = "."
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving dir: %w", err)
	}

	// Split pattern into segments for matching.
	segments := strings.Split(filepath.ToSlash(pattern), "/")

	var matches []string
	err = filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}

		rel, relErr := filepath.Rel(absDir, path)
		if relErr != nil {
			return nil
		}

		// Don't match the root itself.
		if rel == "." {
			return nil
		}

		// Skip hidden directories (but not hidden files in the pattern).
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && !patternWantsHidden(pattern) {
			return filepath.SkipDir
		}

		if matchGlob(segments, strings.Split(filepath.ToSlash(rel), "/")) {
			matches = append(matches, path)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", absDir, err)
	}

	// Sort by modification time, newest first.
	sort.Slice(matches, func(i, j int) bool {
		iInfo, _ := os.Stat(matches[i])
		jInfo, _ := os.Stat(matches[j])
		if iInfo == nil || jInfo == nil {
			return matches[i] < matches[j]
		}
		return iInfo.ModTime().After(jInfo.ModTime())
	})

	return matches, nil
}

// matchGlob matches a split path against split glob segments, supporting **
// for recursive matching.
func matchGlob(pattern, path []string) bool {
	pi, pa := 0, 0
	for pi < len(pattern) && pa < len(path) {
		if pattern[pi] == "**" {
			// ** matches zero or more path segments.
			pi++
			if pi >= len(pattern) {
				return true // trailing ** matches everything
			}
			// Try matching the rest of pattern against every suffix of path.
			for k := pa; k <= len(path); k++ {
				if matchGlob(pattern[pi:], path[k:]) {
					return true
				}
			}
			return false
		}

		matched, _ := filepath.Match(pattern[pi], path[pa])
		if !matched {
			return false
		}
		pi++
		pa++
	}

	// Consume trailing ** patterns.
	for pi < len(pattern) && pattern[pi] == "**" {
		pi++
	}

	return pi >= len(pattern) && pa >= len(path)
}

func patternWantsHidden(pattern string) bool {
	return strings.Contains(pattern, "/.") || strings.HasPrefix(pattern, ".")
}

func formatGlobResults(matches []string) string {
	if len(matches) == 0 {
		return "No files found"
	}
	return strings.Join(matches, "\n")
}
