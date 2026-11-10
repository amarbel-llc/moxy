// Package shadow is the moxy side of ajour shadow mode (moxy#489): it turns a
// moxy tool call into the Call shape of cutting-garden FDR 0028 ("The two
// inputs"), hands it to an Evaluator, and logs the verdict. It enforces
// nothing: Observe returns nothing and never changes a call's outcome.
//
// Call and Subject are a local mirror of ajour's types, kept so this package
// builds without cutting-garden as a dependency (adding one is a flake-input
// change that needs the operator). When pkgs/ajour is linkable, the mirror
// goes away and Evaluator becomes a thin adapter over ajour.Evaluate.
package shadow

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Subject mirrors ajour.Subject (FDR 0028).
type Subject struct {
	Type   string
	ID     string
	New    bool
	Digest string
	Fields map[string][]string
}

// Call mirrors ajour.Call (FDR 0028).
type Call struct {
	Path      []Subject
	Values    [][]Subject
	Resources [][]Subject
	Anchor    int
}

var (
	// ErrPatternEscapes: a glob pattern that is absolute or has a ".."
	// segment; the catalog contract (RFC 0018 section 7) refuses it because
	// the pattern would reach outside the path field.
	ErrPatternEscapes = errors.New("glob pattern is absolute or has a .. segment")
	// ErrUnresolved: an omitted field the catalog says to present resolved
	// could not be resolved.
	ErrUnresolved = errors.New("omitted field cannot be resolved")
	// ErrUnsupported: the call uses a form this builder does not type yet.
	ErrUnsupported = errors.New("unsupported call form")
)

// Env is what the builder needs from the process around the call.
type Env struct {
	// Cwd is the working directory an omitted cwd-resolved field means.
	Cwd string
	// Home makes file: ids home-relative when a path is under it.
	Home string
	// Remote resolves the repo and host an omitted cwd-remote field means.
	Remote func() (repo, host string, ok bool)
	// Facts supplies the tool subject's catalog facts (kind, perm, async,
	// impure) from the moxin manifests; nil presents none.
	Facts func(server, tool string) map[string][]string
}

// Builder builds Calls from tool calls.
type Builder struct {
	env Env
	cat *catalog
}

// NewBuilder loads the embedded catalog.
func NewBuilder(env Env) (*Builder, error) {
	cat, err := loadCatalog()
	if err != nil {
		return nil, err
	}
	return &Builder{env: env, cat: cat}, nil
}

const pluginMoxy = "moxy"

// BuildNamed builds from a rendered tool name under the default name
// template: the server is everything before the first ".", the tool the rest;
// a name with no "." is a moxy meta tool, server "moxy" (RFC 0018 finding).
func (b *Builder) BuildNamed(name string, args map[string]any) ([]Call, error) {
	server, tool := pluginMoxy, name
	if i := strings.Index(name, "."); i >= 0 {
		server, tool = name[:i], name[i+1:]
	}
	return b.Build(server, tool, args)
}

// Build builds the Calls for one dispatched tool call. async and batch
// present the calls they wrap (the catalog's wrapped-call), so one dispatch
// can yield several Calls.
func (b *Builder) Build(server, tool string, args map[string]any) ([]Call, error) {
	if server == pluginMoxy {
		switch tool {
		case "async":
			return b.buildAsync(args)
		case "batch":
			return b.buildBatch(args)
		}
	}
	c, err := b.buildOne(server, tool, args)
	if err != nil {
		return nil, err
	}
	return []Call{c}, nil
}

func (b *Builder) buildAsync(args map[string]any) ([]Call, error) {
	name, _ := args["tool"].(string)
	if name == "" {
		return nil, fmt.Errorf("async: %w: no tool", ErrUnsupported)
	}
	inner, _ := args["args"].(map[string]any)
	return b.BuildNamed(name, inner)
}

func (b *Builder) buildBatch(args map[string]any) ([]Call, error) {
	list, ok := args["calls"].([]any)
	if !ok {
		return nil, fmt.Errorf("batch: %w: calls is not a list", ErrUnsupported)
	}
	var out []Call
	for i, item := range list {
		m, ok := item.(map[string]any)
		name, _ := m["tool"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("batch call %d: %w: no tool", i, ErrUnsupported)
		}
		inner, _ := m["args"].(map[string]any)
		calls, err := b.BuildNamed(name, inner)
		if err != nil {
			return nil, fmt.Errorf("batch call %d: %w", i, err)
		}
		out = append(out, calls...)
	}
	return out, nil
}

func (b *Builder) buildOne(server, tool string, args map[string]any) (Call, error) {
	ct, cataloged := b.cat.tool(pluginMoxy, server, tool)

	toolFields := map[string][]string{}
	if b.env.Facts != nil {
		for k, v := range b.env.Facts(server, tool) {
			toolFields[k] = v
		}
	}
	call := Call{
		Path: []Subject{
			{Type: "clown-plugin-v1", ID: pluginMoxy},
			{Type: "mcp-server-v1", ID: server},
			{Type: "mcp-tool-v1", ID: tool, Fields: toolFields},
		},
		Anchor: 1,
	}

	if server == "folio" && tool == "glob" {
		if p, ok := args["pattern"].(string); ok && patternEscapes(p) {
			return Call{}, fmt.Errorf("folio.glob pattern: %w", ErrPatternEscapes)
		}
	}

	names := make([]string, 0, len(args))
	for k := range args {
		if k == "_meta" {
			continue
		}
		names = append(names, k)
	}
	sort.Strings(names)

	for _, name := range names {
		decl, declared := catalogField{}, false
		if cataloged {
			decl, declared = ct.field(name)
		}
		if err := b.presentField(&call, decl, declared, name, args, args[name]); err != nil {
			return Call{}, err
		}
	}

	if cataloged {
		for _, decl := range ct.Fields {
			if _, given := args[decl.Name]; given || decl.Resolve == "" {
				continue
			}
			if err := b.presentOmitted(&call, decl, args); err != nil {
				return Call{}, err
			}
		}
	}
	return call, nil
}

// presentField presents one given argument as exactly one kind of value.
func (b *Builder) presentField(call *Call, decl catalogField, declared bool, name string, args map[string]any, val any) error {
	if !declared || decl.Kind == "input" || decl.Kind == "" {
		call.Values = append(call.Values, []Subject{inputSubject(name, scalars(val))})
		return nil
	}
	switch decl.Kind {
	case "route":
		return nil // async/batch plumbing; the wrapped calls are presented instead
	case "path":
		strs := scalars(val)
		if decl.Split == "whitespace" {
			var split []string
			for _, s := range strs {
				split = append(split, strings.Fields(s)...)
			}
			strs = split
		}
		base := b.pathBase(decl, args)
		for _, s := range strs {
			if err := b.presentPath(call, name, s, base); err != nil {
				return err
			}
		}
		return nil
	case "content":
		for _, s := range scalars(val) {
			if blobURI.MatchString(s) {
				if err := b.presentResource(call, name, s); err != nil {
					return err
				}
				continue
			}
			call.Values = append(call.Values, []Subject{inputSubject(name, []string{s})})
		}
		return nil
	case "reference":
		for _, s := range scalars(val) {
			if err := b.presentResource(call, name, s); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("field %s: %w: catalog kind %q", name, ErrUnsupported, decl.Kind)
}

// presentOmitted presents a field the caller left out, resolved, as the
// catalog contract requires; it refuses when it cannot resolve.
func (b *Builder) presentOmitted(call *Call, decl catalogField, args map[string]any) error {
	switch decl.Resolve {
	case "cwd":
		if b.env.Cwd == "" {
			return fmt.Errorf("field %s: %w: no working directory", decl.Name, ErrUnresolved)
		}
		return b.presentPath(call, decl.Name, b.env.Cwd, b.env.Cwd)
	case "cwd-remote":
		if b.env.Remote == nil {
			return fmt.Errorf("field %s: %w: no git remote", decl.Name, ErrUnresolved)
		}
		repo, host, ok := b.env.Remote()
		if !ok {
			return fmt.Errorf("field %s: %w: no git remote", decl.Name, ErrUnresolved)
		}
		switch decl.Name {
		case "host":
			call.Values = append(call.Values, []Subject{inputSubject(decl.Name, []string{host})})
		case "repo", "name":
			call.Values = append(call.Values, []Subject{inputSubject(decl.Name, []string{repo})})
		default:
			return fmt.Errorf("field %s: %w: not a repo or host field", decl.Name, ErrUnresolved)
		}
		return nil
	}
	return fmt.Errorf("field %s: %w: resolver %q", decl.Name, ErrUnsupported, decl.Resolve)
}

// pathBase is what a relative path in this field is relative to: another
// field's value when the catalog says so (grit's paths under repo_path), else
// the working directory.
func (b *Builder) pathBase(decl catalogField, args map[string]any) string {
	base := b.env.Cwd
	if decl.RelativeTo != "" && decl.RelativeTo != "repo-root" {
		if v, ok := args[decl.RelativeTo].(string); ok && v != "" {
			base = v
		}
	}
	return base
}

var (
	anyURI  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)
	blobURI = regexp.MustCompile(`^madder://blobs/`)
)

func (b *Builder) presentPath(call *Call, field, value, base string) error {
	if anyURI.MatchString(value) && !strings.HasPrefix(value, "file://") {
		return b.presentResource(call, field, value)
	}
	p := strings.TrimPrefix(strings.TrimPrefix(value, "file://"), "file:")
	if !filepath.IsAbs(p) {
		if base == "" {
			return fmt.Errorf("field %s: %w: relative path and no working directory", field, ErrUnresolved)
		}
		p = filepath.Join(base, p)
	}
	p = filepath.Clean(p)
	id := "file:" + p
	if h := strings.TrimRight(b.env.Home, "/"); h != "" {
		switch {
		case p == h:
			id = "file:."
		case strings.HasPrefix(p, h+"/"):
			id = "file:" + strings.TrimPrefix(p, h+"/")
		}
	}
	call.Values = append(call.Values, []Subject{{
		Type:   "fs-path-v1",
		ID:     id,
		Fields: map[string][]string{"field": {field}},
	}})
	return nil
}

// presentResource presents a URI as a read of the object under a server's
// resource surface (FDR 0028 "A resource is a path"). The scheme's owner
// comes from the catalog; the Call has one Anchor (the dispatching server),
// so a resource owned by another server (a madder blob read through folio is
// moxy/moxy) is still hung under the anchor here. Whether that is the
// intended representation is a question for the FDR 0028 owners.
func (b *Builder) presentResource(call *Call, field, uri string) error {
	i := strings.Index(uri, "://")
	if i < 0 {
		return fmt.Errorf("field %s: %w: not a URI", field, ErrUnsupported)
	}
	if _, ok := b.cat.schemeOwner(uri[:i]); !ok {
		return fmt.Errorf("field %s: %w: no catalog owner for scheme %q", field, ErrUnsupported, uri[:i])
	}
	op := map[string][]string{"op": {"read"}}
	call.Resources = append(call.Resources, []Subject{
		{Type: "mcp-resources-v1", ID: "resources", Fields: copyFields(op)},
		{Type: "mcp-resource-v1", ID: uri, Fields: copyFields(op)},
	})
	return nil
}

func copyFields(m map[string][]string) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, v := range m {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func inputSubject(field string, values []string) Subject {
	return Subject{
		Type:   "tool-input-v1",
		ID:     field,
		Fields: map[string][]string{"field": {field}, "value": values},
	}
}

// scalars flattens one JSON argument to its string values: a list is
// multi-valued, a map is its compact JSON.
func scalars(v any) []string {
	switch x := v.(type) {
	case nil:
		return []string{""}
	case string:
		return []string{x}
	case bool:
		return []string{strconv.FormatBool(x)}
	case float64:
		return []string{strconv.FormatFloat(x, 'f', -1, 64)}
	case []any:
		var out []string
		for _, e := range x {
			out = append(out, scalars(e)...)
		}
		return out
	default:
		raw, err := json.Marshal(x)
		if err != nil {
			return []string{fmt.Sprint(x)}
		}
		return []string{string(raw)}
	}
}

// patternEscapes reports a glob pattern with an absolute alternative or a
// ".." segment. It reads brace alternatives structurally: an alternative that
// begins with "/" follows a "{" or ",".
func patternEscapes(p string) bool {
	if strings.HasPrefix(p, "/") || strings.Contains(p, "{/") || strings.Contains(p, ",/") {
		return true
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool {
		return r == '/' || r == '{' || r == '}' || r == ','
	}) {
		if seg == ".." {
			return true
		}
	}
	return false
}
