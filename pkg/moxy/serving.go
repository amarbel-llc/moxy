package moxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"code.linenisgreat.com/moxy/pkg/moxyfile"
	"code.linenisgreat.com/moxy/pkg/narrowing"
)

// WideningError is a refusal to serve: the effective moxyfile grants more
// than the parent reference allows. Its message carries one line per
// violation so the refusal is legible wherever it surfaces — Open's error,
// the restart tool's result on a reload.
type WideningError struct {
	Parent     string
	Violations []narrowing.Violation
}

func (e *WideningError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "effective moxyfile does not narrow %s:", e.Parent)
	for _, v := range e.Violations {
		b.WriteString("\n")
		b.WriteString(v.String())
	}
	return b.String()
}

// LoadParentReference reads a parent reference the way `MOXY_PARENT_MOXYFILE`
// is read: an absolute path to a non-empty moxyfile, loaded literally with
// its includes. A reference with no resolved-moxins record is given an empty
// one: for a process about to serve, "no record" must not mean "any moxin",
// since the default MOXIN_PATH includes the working directory's own
// .moxy/moxins (FDR 0013). An empty file is refused rather than obeyed — it
// would constrain almost nothing and is far likelier a render that failed.
func LoadParentReference(path string) (*moxyfile.Config, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%s is not an absolute path", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, fmt.Errorf("%s is empty", path)
	}
	parent, err := moxyfile.LoadFileLiteral(path)
	if err != nil {
		return nil, err
	}
	cfg := parent.Merged
	if cfg.ResolvedMoxins == nil {
		cfg.ResolvedMoxins = map[string]string{}
	}
	return &cfg, nil
}

// source is where the effective moxyfile comes from, re-read on every
// reload. A Config source reloads to itself.
type source struct {
	home string
	dir  string
	file string
	cfg  *moxyfile.Config
}

func newSource(o Options) (source, error) {
	set := 0
	for _, on := range []bool{o.Dir != "", o.File != "", o.Config != nil} {
		if on {
			set++
		}
	}
	if set > 1 {
		return source{}, fmt.Errorf("moxy: Options.Dir, File and Config are mutually exclusive")
	}
	if o.Config != nil {
		return source{cfg: o.Config}, nil
	}
	if o.File != "" {
		return source{file: o.File}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return source{}, err
	}
	dir := o.Dir
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return source{}, err
		}
	}
	return source{home: home, dir: dir}, nil
}

// load reads the effective moxyfile in its literal form (header $VARs
// unexpanded), so it can be judged as written and then served expanded.
func (s source) load() (moxyfile.Config, error) {
	switch {
	case s.cfg != nil:
		return *s.cfg, nil
	case s.file != "":
		h, err := moxyfile.LoadFileLiteral(s.file)
		return h.Merged, err
	default:
		h, err := moxyfile.LoadHierarchyLiteral(s.home, s.dir)
		return h.Merged, err
	}
}

// dirForDiscovery is the directory moxin discovery resolves the default
// MOXIN_PATH hierarchy against: the walked Dir, else the working directory.
func (s source) dirForDiscovery() string {
	if s.dir != "" {
		return s.dir
	}
	cwd, _ := os.Getwd()
	return cwd
}

// serving is one instance's judging state: the parent reference pinned for
// its life, and the allow-tools baseline pinned at the first judging that
// decides what is served (moxy#461). Per instance rather than per process,
// so two instances in one process (tests, an embedder) judge independently.
type serving struct {
	src         source
	parent      *moxyfile.Config
	parentLabel string

	mu      sync.Mutex
	pinned  bool
	startup moxyfile.AllowToolSet
}

func newServing(o Options) (*serving, error) {
	src, err := newSource(o)
	if err != nil {
		return nil, err
	}
	label := o.ParentLabel
	if label == "" {
		label = "the parent reference"
	}
	return &serving{src: src, parent: o.Parent, parentLabel: label}, nil
}

// judgesMoxins reports whether the parent reference bounds which moxins may
// be served, i.e. whether working out the moxin set is worth it before
// refuseUnlessNarrows.
func (s *serving) judgesMoxins() bool {
	return s.parent != nil && s.parent.ResolvedMoxins != nil
}

// refuseUnlessNarrows judges literal against the parent reference. moxins
// is the name-to-directory set the caller is about to serve; it stands in
// for whatever resolved-moxins record the moxyfile itself carries, which is
// never trusted. pin says whether this call, if it succeeds, records the
// startup allow-tools baseline (the first time it has not been yet): true
// from the calls that decide what is actually served, false from a
// preflight read.
func (s *serving) refuseUnlessNarrows(literal moxyfile.Config, moxins map[string]string, pin bool) error {
	if s.parent == nil {
		return nil
	}
	judged := literal
	judged.ResolvedMoxins = moxins
	s.mu.Lock()
	pinned, startup := s.pinned, s.startup
	s.mu.Unlock()
	if violations := narrowing.CheckReload(*s.parent, judged, pinned, startup); len(violations) > 0 {
		return &WideningError{Parent: s.parentLabel, Violations: violations}
	}
	if pin {
		s.mu.Lock()
		if !s.pinned {
			s.pinned, s.startup = true, judged.BuildAllowToolSet()
		}
		s.mu.Unlock()
	}
	return nil
}
