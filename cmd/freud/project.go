package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// projectInfo describes one directory under ~/.claude/projects/.
type projectInfo struct {
	dirName   string    // raw directory name on disk
	absPath   string    // resolved absolute path of the project's cwd, or empty
	heuristic bool      // true when absPath came from name decoding, not JSONL
	dirMtime  time.Time // mtime of the project directory at scan time
}

// projectCache memoises projectInfo entries keyed by directory mtime, so a
// repeat scan only re-parses JSONL files for directories that changed.
type projectCache struct {
	mu      sync.Mutex
	entries map[string]projectInfo
}

func newProjectCache() *projectCache {
	return &projectCache{entries: make(map[string]projectInfo)}
}

// scanProjects walks projectsDir, returning one projectInfo per child
// directory. Directories whose mtime is unchanged since the last scan are
// served from cache; the rest are re-resolved by reading their newest JSONL.
//
// Missing or unreadable projectsDir yields an empty slice with no error — the
// caller (e.g. an empty $HOME in tests) treats that as "no sessions yet".
func (c *projectCache) scanProjects(projectsDir string) ([]projectInfo, error) {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	var out []projectInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		dirPath := filepath.Join(projectsDir, dirName)

		stat, statErr := os.Stat(dirPath)
		if statErr != nil {
			continue
		}

		mtime := stat.ModTime()
		if cached, ok := c.entries[dirName]; ok && cached.dirMtime.Equal(mtime) {
			out = append(out, cached)
			continue
		}

		info := projectInfo{dirName: dirName, dirMtime: mtime}
		if abs, ok := resolveCWDFromNewestJSONL(dirPath); ok {
			info.absPath = abs
		} else {
			info.absPath = decodeProjectDirName(dirName)
			info.heuristic = true
		}

		c.entries[dirName] = info
		out = append(out, info)
	}

	return out, nil
}

// resolveCWDFromNewestJSONL opens the newest JSONL in projectDirPath and
// returns the first non-empty cwd field it finds, scanning at most
// maxLinesScanned lines to avoid pulling an entire huge transcript into
// memory just to discover its project path.
func resolveCWDFromNewestJSONL(projectDirPath string) (string, bool) {
	const maxLinesScanned = 200

	files, err := os.ReadDir(projectDirPath)
	if err != nil {
		return "", false
	}

	type jsonlFile struct {
		path  string
		mtime time.Time
	}
	var jsonls []jsonlFile
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		stat, statErr := os.Stat(filepath.Join(projectDirPath, f.Name()))
		if statErr != nil {
			continue
		}
		jsonls = append(jsonls, jsonlFile{
			path:  filepath.Join(projectDirPath, f.Name()),
			mtime: stat.ModTime(),
		})
	}
	sort.Slice(jsonls, func(i, j int) bool {
		return jsonls[i].mtime.After(jsonls[j].mtime)
	})

	for _, jf := range jsonls {
		if cwd, ok := firstCWD(jf.path, maxLinesScanned); ok {
			return cwd, true
		}
	}
	return "", false
}

// firstCWD reads up to maxLines lines from path and returns the first
// non-empty cwd field found in any JSON object.
func firstCWD(path string, maxLines int) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Allow long lines — Claude messages with embedded tool results can
	// easily exceed bufio's default 64KB.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	count := 0
	for scanner.Scan() {
		count++
		if count > maxLines {
			break
		}
		line := scanner.Bytes()
		// Cheap fast-path: skip lines that don't even mention "cwd".
		if !bytes.Contains(line, []byte(`"cwd"`)) {
			continue
		}
		var probe struct {
			CWD string `json:"cwd"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			continue
		}
		if probe.CWD != "" {
			return probe.CWD, true
		}
	}
	return "", false
}

// decodeProjectDirName approximates the inverse of Claude Code's project
// directory naming, which replaces every "/" and "." in the cwd with "-".
// The encoding is lossy: a real hyphen and a dot both become "-", so this
// fallback is only used when the JSONL itself yields no usable cwd.
//
// Strategy: prepend "/" to recover the leading absolute slash, then turn
// every remaining "-" into "/". Dots are not recovered. The result is
// always rooted at "/" and surfaced to agents with a "(heuristic)" marker
// so they know it's approximate.
func decodeProjectDirName(name string) string {
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "-") {
		name = name[1:]
	}
	return "/" + strings.ReplaceAll(name, "-", "/")
}
