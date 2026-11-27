// Package folioglob is folio.glob's matcher (moxy#532). The permission check
// judges the directory alone, so the matcher must not be able to leave it:
// every lookup goes through an os.Root, which resolves no name outside its
// directory. A shell expansion filtered afterwards cannot promise that.
//
// Patterns keep the semantics the tool had under bash (#514): brace
// alternatives, "**" for any depth (a linked directory is not entered by it),
// and dotfiles matched like any other name.
package folioglob

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"time"
)

// maxAlternatives bounds brace expansion, and maxSegments the depth of one
// alternative.
const (
	maxAlternatives = 1024
	maxSegments     = 256
)

// ErrEscapes refuses a pattern that spells a way out of the directory.
var ErrEscapes = errors.New("glob pattern must stay under path: absolute patterns and '..' segments are refused. Pass that directory as path instead")

// Result is what a pattern matched under the directory.
type Result struct {
	// Files are the matching regular files, relative to the directory, newest
	// first.
	Files []string
	// Dirs counts the matching directories, which are not listed.
	Dirs int
	// LinkNotSearched reports that matching met a link the root would not
	// follow: one that leads outside the directory, or an absolute one. It
	// says nothing about what the link leads to, which is never looked at.
	LinkNotSearched bool
}

type matcher struct {
	root  *os.Root
	files map[string]time.Time
	dirs  map[string]bool
	seen  map[string]bool // directory + remaining segments already walked
	left  bool
}

// Match matches pattern under root. A malformed pattern is an error.
func Match(root *os.Root, pattern string) (Result, error) {
	alternatives, err := expandBraces(pattern, nil)
	if err != nil {
		return Result{}, err
	}
	type compiled struct {
		segments []string
		dirOnly  bool
	}
	var all []compiled
	// every alternative is checked before anything is read
	for _, alt := range alternatives {
		if strings.HasPrefix(alt, "/") {
			return Result{}, ErrEscapes
		}
		c := compiled{dirOnly: strings.HasSuffix(alt, "/")}
		for _, seg := range strings.Split(alt, "/") {
			seg = classNegation(seg)
			switch {
			case seg == "" || unescape(seg) == ".":
				continue
			case unescape(seg) == "..":
				return Result{}, ErrEscapes
			case strings.Contains(seg, "[[:"):
				return Result{}, fmt.Errorf("unsupported glob pattern: POSIX character classes are not supported, write a range such as [A-Za-z]: %s", pattern)
			case seg == "**" && len(c.segments) > 0 && c.segments[len(c.segments)-1] == "**":
				continue // "**/**" matches what "**" does, at a higher cost
			}
			if _, err := path.Match(seg, ""); err != nil {
				return Result{}, fmt.Errorf("unsupported glob pattern: malformed %q (to match a bracket itself, write \\[): %s", seg, pattern)
			}
			if len(c.segments) == maxSegments {
				return Result{}, fmt.Errorf("unsupported glob pattern: more than %d segments; give a deeper directory as path", maxSegments)
			}
			c.segments = append(c.segments, seg)
		}
		all = append(all, c)
	}

	m := &matcher{root: root, files: map[string]time.Time{}, dirs: map[string]bool{}, seen: map[string]bool{}}
	for _, c := range all {
		if len(c.segments) == 0 {
			if c.dirOnly || pattern != "" {
				m.dirs["."] = true
			}
			continue
		}
		m.walk(".", c.segments, c.dirOnly)
	}

	res := Result{Dirs: len(m.dirs), LinkNotSearched: m.left}
	for name := range m.files {
		res.Files = append(res.Files, name)
	}
	slices.SortFunc(res.Files, func(a, b string) int {
		if c := m.files[b].Compare(m.files[a]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return res, nil
}

// walk matches segments, which is not empty, against the entries of dir.
func (m *matcher) walk(dir string, segments []string, dirOnly bool) {
	key := fmt.Sprintf("%t\x00%s\x00%s", dirOnly, dir, strings.Join(segments, "/"))
	if m.seen[key] {
		return
	}
	m.seen[key] = true
	seg, rest := segments[0], segments[1:]

	if seg == "**" {
		if len(rest) > 0 {
			m.walk(dir, rest, dirOnly) // "**" standing for no directory at all
		}
		for _, entry := range m.entries(dir) {
			name := join(dir, entry.Name())
			if len(rest) == 0 {
				m.found(name, dirOnly)
			}
			if entry.IsDir() { // a link is not a directory here: not entered
				m.walk(name, segments, dirOnly)
			}
		}
		return
	}

	step := func(name string) {
		if len(rest) == 0 {
			m.found(name, dirOnly)
		} else if info, ok := m.stat(name); ok && info.IsDir() {
			m.walk(name, rest, dirOnly)
		}
	}
	if !hasMeta(seg) {
		step(join(dir, unescape(seg)))
		return
	}
	for _, entry := range m.entries(dir) {
		if ok, _ := path.Match(seg, entry.Name()); ok {
			step(join(dir, entry.Name()))
		}
	}
}

// entries lists dir; an unreadable directory has none.
func (m *matcher) entries(dir string) []fs.DirEntry {
	f, err := m.root.Open(dir)
	if err != nil {
		return nil
	}
	defer f.Close()
	entries, _ := f.ReadDir(-1)
	return entries
}

// stat follows links inside the root only. A link it cannot follow for any
// reason but a missing target is noted.
func (m *matcher) stat(name string) (fs.FileInfo, bool) {
	info, err := m.root.Stat(name)
	if err == nil {
		return info, true
	}
	if !errors.Is(err, fs.ErrNotExist) {
		if li, lerr := m.root.Lstat(name); lerr == nil && li.Mode()&fs.ModeSymlink != 0 {
			m.left = true
		}
	}
	return nil, false
}

func (m *matcher) found(name string, dirOnly bool) {
	info, ok := m.stat(name)
	switch {
	case !ok:
	case info.IsDir():
		m.dirs[name] = true
	case info.Mode().IsRegular() && !dirOnly:
		m.files[name] = info.ModTime()
	}
}

func join(dir, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}

// classNegation rewrites the shell's "[!...]" as path.Match's "[^...]",
// leaving an escaped "\[!" alone.
func classNegation(seg string) string {
	b := []byte(seg)
	for i := 0; i+1 < len(b); i++ {
		switch {
		case b[i] == '\\':
			i++
		case b[i] == '[' && b[i+1] == '!':
			b[i+1] = '^'
		}
	}
	return string(b)
}

func hasMeta(seg string) bool {
	for i := 0; i < len(seg); i++ {
		switch seg[i] {
		case '\\':
			i++
		case '*', '?', '[':
			return true
		}
	}
	return false
}

// unescape drops the backslashes that quote the next character.
func unescape(seg string) string {
	var b strings.Builder
	for i := 0; i < len(seg); i++ {
		if seg[i] == '\\' && i+1 < len(seg) {
			i++
		}
		b.WriteByte(seg[i])
	}
	return b.String()
}

// expandBraces appends each brace alternative of s to out. "{a}" and "{}"
// (no top-level comma) stay literal; an unclosed "{" is an error.
func expandBraces(s string, out []string) ([]string, error) {
	depth, start, end := 0, -1, -1
	var commas []int
scan:
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			if depth == 0 {
				start, commas = i, nil
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && len(commas) > 0 {
				end = i
				break scan
			}
		case ',':
			if depth == 1 {
				commas = append(commas, i)
			}
		}
	}
	if end < 0 {
		if depth > 0 {
			return nil, fmt.Errorf("unsupported glob pattern: unclosed '{' in brace expression: %s", s)
		}
		if len(out) >= maxAlternatives {
			return nil, fmt.Errorf("unsupported glob pattern: more than %d brace alternatives; split the pattern into several calls", maxAlternatives)
		}
		return append(out, s), nil
	}
	pre, post, from := s[:start], s[end+1:], start+1
	for _, comma := range append(commas, end) {
		var err error
		if out, err = expandBraces(pre+s[from:comma]+post, out); err != nil {
			return nil, err
		}
		from = comma + 1
	}
	return out, nil
}
