package shadow

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func testEnv() Env {
	return Env{
		Cwd:  "/home/u/repo/wt",
		Home: "/home/u",
		Remote: func() (string, string, bool) {
			return "owner/name", "forge.example", true
		},
	}
}

func newTestBuilder(t *testing.T, env Env) *Builder {
	t.Helper()
	b, err := NewBuilder(env)
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	return b
}

type built struct {
	calls []Call
	err   error
}

func onlyCall(t *testing.T, r built) Call {
	t.Helper()
	if r.err != nil {
		t.Fatalf("Build: %v", r.err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(r.calls), r.calls)
	}
	return r.calls[0]
}

func pathIDs(c Call) []string {
	var ids []string
	for _, s := range c.Path {
		ids = append(ids, s.ID)
	}
	return ids
}

// findValue returns the single-step value path whose first subject has the
// given Type and field name, or fails.
func findValue(t *testing.T, c Call, typ, field string) Subject {
	t.Helper()
	for _, v := range c.Values {
		if len(v) == 0 {
			continue
		}
		s := v[0]
		if s.Type == typ && len(s.Fields["field"]) == 1 && s.Fields["field"][0] == field {
			return s
		}
	}
	t.Fatalf("no %s value for field %q in %+v", typ, field, c.Values)
	return Subject{}
}

func TestPathIsPluginServerTool(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "man.toc", map[string]any{"page": "ls"}))
	if got, want := pathIDs(c), []string{"moxy", "man", "toc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("path = %v, want %v", got, want)
	}
	wantTypes := []string{"clown-plugin-v1", "mcp-server-v1", "mcp-tool-v1"}
	for i, s := range c.Path {
		if s.Type != wantTypes[i] {
			t.Errorf("path[%d].Type = %q, want %q", i, s.Type, wantTypes[i])
		}
	}
	if c.Anchor != 1 {
		t.Errorf("Anchor = %d, want 1 (the server)", c.Anchor)
	}
}

func TestNameSplitsAtFirstDot(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "smith.issue-get", map[string]any{"number": 1.0, "repo": "o/n", "host": "h"}))
	if got, want := pathIDs(c), []string{"moxy", "smith", "issue-get"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("path = %v, want %v", got, want)
	}
	// A tool name that itself contains a dot stays whole after the first one.
	c = onlyCall(t, mustBuild(b, "srv.a.b", map[string]any{}))
	if got, want := pathIDs(c), []string{"moxy", "srv", "a.b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("path = %v, want %v", got, want)
	}
}

func TestPrefixlessMetaToolsAreServerMoxy(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "status", map[string]any{}))
	if got, want := pathIDs(c), []string{"moxy", "moxy", "status"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("path = %v, want %v", got, want)
	}
}

func TestPathFieldIsFsPathHomeRelative(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "folio.read", map[string]any{"file_path": "/home/u/repo/wt/a.go", "start": 3.0}))
	s := findValue(t, c, "fs-path-v1", "file_path")
	if s.ID != "file:repo/wt/a.go" {
		t.Errorf("ID = %q, want file:repo/wt/a.go", s.ID)
	}
	st := findValue(t, c, "tool-input-v1", "start")
	if st.ID != "start" || !reflect.DeepEqual(st.Fields["value"], []string{"3"}) {
		t.Errorf("start subject = %+v", st)
	}
	// one kind per field: file_path is not also a tool-input-v1.
	for _, v := range c.Values {
		for _, sub := range v {
			if sub.Type == "tool-input-v1" && sub.Fields["field"][0] == "file_path" {
				t.Errorf("file_path presented as tool-input too: %+v", sub)
			}
		}
	}
}

func TestPathOutsideHomeStaysAbsolute(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "folio.read", map[string]any{"file_path": "/nix/store/x/../y/f"}))
	s := findValue(t, c, "fs-path-v1", "file_path")
	if s.ID != "file:/nix/store/y/f" {
		t.Errorf("ID = %q, want file:/nix/store/y/f (canonical, absolute)", s.ID)
	}
}

func TestRelativePathResolvesAgainstCwd(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "folio.read", map[string]any{"file_path": "sub/../a.go"}))
	if s := findValue(t, c, "fs-path-v1", "file_path"); s.ID != "file:repo/wt/a.go" {
		t.Errorf("ID = %q, want file:repo/wt/a.go", s.ID)
	}
}

func TestOmittedCwdFieldIsPresentedResolved(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "rg.search", map[string]any{"pattern": "foo"}))
	if s := findValue(t, c, "fs-path-v1", "path"); s.ID != "file:repo/wt" {
		t.Errorf("ID = %q, want file:repo/wt", s.ID)
	}
	if s := findValue(t, c, "tool-input-v1", "pattern"); !reflect.DeepEqual(s.Fields["value"], []string{"foo"}) {
		t.Errorf("pattern subject = %+v", s)
	}
}

func TestGlobRefusesEscapingPattern(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	for _, p := range []string{"/etc/*", "../x/*", "a/../../b", "{a,/b}/*"} {
		_, err := b.BuildNamed("folio.glob", map[string]any{"pattern": p})
		if !errors.Is(err, ErrPatternEscapes) {
			t.Errorf("pattern %q: err = %v, want ErrPatternEscapes", p, err)
		}
	}
	for _, p := range []string{"**/*.go", "src/**/*.{go,rs}", "vendor/*", "a..b/*"} {
		if _, err := b.BuildNamed("folio.glob", map[string]any{"pattern": p}); err != nil {
			t.Errorf("pattern %q: unexpected err %v", p, err)
		}
	}
}

func TestNonFileURIInPathFieldIsResourceReadNotFsPath(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	uri := "madder://blobs/blake2b256-abc"
	c := onlyCall(t, mustBuild(b, "folio.read", map[string]any{"file_path": uri}))
	for _, v := range c.Values {
		for _, s := range v {
			if s.Type == "fs-path-v1" {
				t.Fatalf("non-file URI presented as fs-path-v1: %+v", s)
			}
		}
	}
	if len(c.Resources) != 1 {
		t.Fatalf("Resources = %+v, want one path", c.Resources)
	}
	r := c.Resources[0]
	if len(r) != 2 || r[0].Type != "mcp-resources-v1" || r[1].Type != "mcp-resource-v1" || r[1].ID != uri {
		t.Fatalf("resource path = %+v", r)
	}
	for _, s := range r {
		if !reflect.DeepEqual(s.Fields["op"], []string{"read"}) {
			t.Errorf("step %+v lacks op=[read]", s)
		}
	}
}

func TestSmithOmittedRepoAndHostResolveFromRemote(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "smith.issue-get", map[string]any{"number": 7.0}))
	if s := findValue(t, c, "tool-input-v1", "repo"); !reflect.DeepEqual(s.Fields["value"], []string{"owner/name"}) {
		t.Errorf("repo = %+v", s)
	}
	if s := findValue(t, c, "tool-input-v1", "host"); !reflect.DeepEqual(s.Fields["value"], []string{"forge.example"}) {
		t.Errorf("host = %+v", s)
	}
}

func TestSmithGivenRepoIsNotOverridden(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "smith.issue-get", map[string]any{"number": 7.0, "repo": "x/y", "host": "h"}))
	if s := findValue(t, c, "tool-input-v1", "repo"); !reflect.DeepEqual(s.Fields["value"], []string{"x/y"}) {
		t.Errorf("repo = %+v", s)
	}
}

func TestUnresolvableOmittedFieldRefuses(t *testing.T) {
	env := testEnv()
	env.Remote = func() (string, string, bool) { return "", "", false }
	b := newTestBuilder(t, env)
	_, err := b.BuildNamed("smith.issue-get", map[string]any{"number": 7.0})
	if !errors.Is(err, ErrUnresolved) {
		t.Fatalf("err = %v, want ErrUnresolved", err)
	}
	env = testEnv()
	env.Cwd = ""
	b = newTestBuilder(t, env)
	_, err = b.BuildNamed("rg.search", map[string]any{"pattern": "x"})
	if !errors.Is(err, ErrUnresolved) {
		t.Fatalf("err = %v, want ErrUnresolved for empty cwd", err)
	}
}

func TestSplitWhitespacePathsField(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "folio.rm", map[string]any{"paths": "/home/u/a /home/u/b", "force": true}))
	var ids []string
	for _, v := range c.Values {
		for _, s := range v {
			if s.Type == "fs-path-v1" {
				ids = append(ids, s.ID)
			}
		}
	}
	if want := []string{"file:a", "file:b"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("fs paths = %v, want %v", ids, want)
	}
}

func TestListValuedPathsField(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "grit.status", map[string]any{"paths": []any{"a.go", "dir/b.go"}}))
	var ids []string
	for _, v := range c.Values {
		for _, s := range v {
			if s.Type == "fs-path-v1" {
				ids = append(ids, s.ID)
			}
		}
	}
	if want := []string{"file:repo/wt/a.go", "file:repo/wt/dir/b.go"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("fs paths = %v, want %v", ids, want)
	}
}

func TestAsyncPresentsTheWrappedCall(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	calls, err := b.BuildNamed("async", map[string]any{
		"tool":    "man.toc",
		"args":    map[string]any{"page": "ls"},
		"timeout": "10m",
	})
	c := onlyCall(t, built{calls, err})
	if got, want := pathIDs(c), []string{"moxy", "man", "toc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("wrapped path = %v, want %v", got, want)
	}
	if len(c.Values) != 1 {
		t.Errorf("wrapper fields (tool/args/timeout) leaked into values: %+v", c.Values)
	}
}

func TestBatchPresentsEachWrappedCall(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	calls, err := b.BuildNamed("batch", map[string]any{
		"calls": []any{
			map[string]any{"tool": "man.toc", "args": map[string]any{"page": "ls"}},
			map[string]any{"tool": "folio.read", "args": map[string]any{"file_path": "/home/u/x"}},
		},
		"on_error": "stop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[1].Path[2].ID != "read" {
		t.Errorf("second call path = %v", pathIDs(calls[1]))
	}
}

func TestMalformedWrappedCallRefuses(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	if _, err := b.BuildNamed("async", map[string]any{"args": map[string]any{}}); err == nil {
		t.Error("async without tool: want error")
	}
	if _, err := b.BuildNamed("batch", map[string]any{"calls": "nope"}); err == nil {
		t.Error("batch with non-list calls: want error")
	}
}

func TestUncatalogedToolIsAllToolInput(t *testing.T) {
	b := newTestBuilder(t, testEnv())
	c := onlyCall(t, mustBuild(b, "newsrv.thing", map[string]any{"a": "x", "b": 2.0}))
	if len(c.Values) != 2 {
		t.Fatalf("values = %+v", c.Values)
	}
	for _, v := range c.Values {
		if v[0].Type != "tool-input-v1" {
			t.Errorf("subject %+v is not tool-input-v1", v[0])
		}
	}
}

func TestToolFactsRideOnTheToolSubject(t *testing.T) {
	env := testEnv()
	env.Facts = func(server, tool string) map[string][]string {
		if server == "man" && tool == "toc" {
			return map[string][]string{"perm": {"allow"}}
		}
		return nil
	}
	b := newTestBuilder(t, env)
	c := onlyCall(t, mustBuild(b, "man.toc", map[string]any{"page": "ls"}))
	if got := c.Path[2].Fields["perm"]; !reflect.DeepEqual(got, []string{"allow"}) {
		t.Errorf("tool Fields = %+v", c.Path[2].Fields)
	}
}

func TestEmbeddedCatalogParses(t *testing.T) {
	cat, err := loadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cat.tool("moxy", "folio", "glob"); !ok {
		t.Error("catalog lacks moxy/folio/glob")
	}
	if owner, ok := cat.schemeOwner("madder"); !ok || owner.Plugin != "moxy" || owner.Server != "moxy" {
		t.Errorf("madder owner = %+v, %v", owner, ok)
	}
	if strings.TrimSpace(catalogSource) == "" {
		t.Error("catalog not embedded")
	}
}

func mustBuild(b *Builder, name string, args map[string]any) built {
	calls, err := b.BuildNamed(name, args)
	return built{calls, err}
}
