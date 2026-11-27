package folioglob

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// fixture: base/wt is the directory; base/out is outside it.
func fixture(t *testing.T) (string, *os.Root) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range []string{
		"wt/a.go", "wt/sub/b.rs", "wt/sub/deep/c.go", "wt/.hidden/h.go", "wt/[x].txt",
		"out/secretdir/outside.go",
	} {
		full := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(full, old, old); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(filepath.Join(base, "wt"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return base, root
}

func link(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}

func TestMatch(t *testing.T) {
	_, root := fixture(t)
	for pattern, want := range map[string][]string{
		"*.go":              {"a.go"},
		"./*.go":            {"a.go"},
		"**/*.go":           {".hidden/h.go", "a.go", "sub/deep/c.go"},
		"**/*.{go,rs}":      {".hidden/h.go", "a.go", "sub/b.rs", "sub/deep/c.go"},
		"sub/**":            {"sub/b.rs", "sub/deep/c.go"},
		"sub/**/**/*.go":    {"sub/deep/c.go"},
		"{a,a}.go":          {"a.go"},
		"[!z].go":           {"a.go"},
		`\[x\].txt`:         {"[x].txt"},
		"sub/deep/c.go":     {"sub/deep/c.go"},
		"nope/*":            nil,
		"x\n/**":            nil,
		"x\n/etc/*":         nil,
		"":                  nil,
		"[.][.]/*/*/*.go":   nil,
		".*/secretdir/*.go": nil,
		"*/../*":            nil, // refused, see below
	} {
		res, err := Match(root, pattern)
		if pattern == "*/../*" {
			if !errors.Is(err, ErrEscapes) {
				t.Errorf("Match(%q) err = %v, want ErrEscapes", pattern, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Match(%q): %v", pattern, err)
			continue
		}
		got := slices.Clone(res.Files)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("Match(%q) = %q, want %q", pattern, got, want)
		}
		if res.LinkNotSearched {
			t.Errorf("Match(%q) reports a link where there is none", pattern)
		}
	}
}

func TestMatchRefusesEverySpellingOfAWayOut(t *testing.T) {
	_, root := fixture(t)
	for _, pattern := range []string{
		"/etc/*", "..", "../*", "sub/../../out/*", "{sub,../out}/*", "{sub,/etc}/*", ".{.,x}/out/*",
		`\.\./out/*`, `.\./out/*`, `\.\./out/*/\.\./\.\./wt/*`,
	} {
		if _, err := Match(root, pattern); !errors.Is(err, ErrEscapes) {
			t.Errorf("Match(%q) err = %v, want ErrEscapes", pattern, err)
		}
	}
}

func TestMatchMalformed(t *testing.T) {
	_, root := fixture(t)
	for _, pattern := range []string{"*.{go,rs", "a[", "[[:alpha:]]*"} {
		if _, err := Match(root, pattern); err == nil {
			t.Errorf("Match(%q) succeeded", pattern)
		}
	}
}

func TestMatchNeverFollowsALinkOut(t *testing.T) {
	base, root := fixture(t)
	wt := filepath.Join(base, "wt")
	link(t, "../out", filepath.Join(wt, "way-out"))
	link(t, filepath.Join(base, "out"), filepath.Join(wt, "way-out-abs"))
	// a link outside that leads back in must not be reachable through way-out
	link(t, wt, filepath.Join(base, "out", "secretdir", "back"))
	for _, pattern := range []string{
		"way-out/*", "way-out/**", "way-out/secretdir/*.go", "way-out/*/back/*.go",
		"way-out-abs/*", "*/secretdir/*", "way-out/", "way-out/no-such*",
	} {
		res, err := Match(root, pattern)
		if err != nil {
			t.Fatalf("Match(%q): %v", pattern, err)
		}
		if len(res.Files) != 0 || res.Dirs != 0 {
			t.Errorf("Match(%q) = %q and %d dirs, want nothing", pattern, res.Files, res.Dirs)
		}
		if !res.LinkNotSearched {
			t.Errorf("Match(%q) does not report the link it did not search", pattern)
		}
	}
}

func TestMatchFollowsAnInsideLinkButNotWithDoubleStar(t *testing.T) {
	base, root := fixture(t)
	wt := filepath.Join(base, "wt")
	link(t, "sub", filepath.Join(wt, "sub-link"))
	link(t, "no-such", filepath.Join(wt, "dangling"))
	res, err := Match(root, "sub-link/*")
	if err != nil || !slices.Equal(res.Files, []string{"sub-link/b.rs"}) || res.LinkNotSearched {
		t.Errorf("sub-link/* = %+v, %v", res, err)
	}
	res, err = Match(root, "**/b.rs")
	if err != nil || !slices.Equal(res.Files, []string{"sub/b.rs"}) {
		t.Errorf("**/b.rs = %+v, %v", res, err)
	}
	res, err = Match(root, "dangling*")
	if err != nil || len(res.Files) != 0 || res.LinkNotSearched {
		t.Errorf("a dangling inside link = %+v, %v", res, err)
	}
}

func TestMatchNewestFirstAndCountsDirectories(t *testing.T) {
	base, root := fixture(t)
	now := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(base, "wt", "sub", "deep", "c.go"), now, now); err != nil {
		t.Fatal(err)
	}
	res, err := Match(root, "**/*.go")
	if err != nil || !slices.Equal(res.Files, []string{"sub/deep/c.go", ".hidden/h.go", "a.go"}) {
		t.Errorf("order = %+v, %v", res, err)
	}
	res, err = Match(root, "sub/*/")
	if err != nil || len(res.Files) != 0 || res.Dirs != 1 {
		t.Errorf("sub/*/ = %+v, %v", res, err)
	}
}
