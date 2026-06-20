// Package naming renders and resolves the namespaced names moxy advertises for
// child-server tools and prompts.
//
// By default a child tool named "commit" on server "grit" is advertised as
// "grit.commit" (the historical dot join). The template is configurable —
// e.g. "{server}_{tool}" yields "grit_commit", a name that satisfies strict
// frontends (claude.ai validates tool names against ^[a-zA-Z0-9_-]{1,64}$ and
// rejects any dot). Because an arbitrary template is not parseable back to its
// inputs, dispatch does not re-parse the rendered string: a Registry built at
// registration time maps each rendered name back to its canonical Entry
// (server + the child's own tool/prompt name + exposure category).
//
// Parse rule asymmetry (intentional — do not "fix"): {tool} is REQUIRED, but
// {server} is OPTIONAL. Dropping {tool} would collapse every tool of a server
// to one name — an intra-server collision that is never wanted and is cheap to
// reject statically. Dropping {server} (e.g. the bare "{tool}" template) is a
// legitimate request whose collisions are data-dependent (two servers sharing a
// tool name) and are caught at Builder.Build time, not at Parse time.
//
// Resource URIs are out of scope: they keep their "<server>/<uri>" form and are
// routed by the proxy's own slash split, never through this package.
package naming

import (
	"fmt"
	"regexp"
	"strings"
)

// Kind distinguishes the two namespaced surfaces this package renders. Tools and
// prompts get independent registries (a tool and a prompt may share a rendered
// name without conflict — they are dispatched by different MCP methods).
type Kind int

const (
	// KindTool is a child-server tool name.
	KindTool Kind = iota
	// KindPrompt is a child-server prompt name.
	KindPrompt
)

// Category is the exposure class carried on an Entry so the proxy can apply the
// --expose tool-exposure filter without re-parsing the rendered name (which is
// impossible under a custom template). It mirrors toolfilter.Category; the proxy
// maps between the two at its single filter call site to avoid an import cycle.
type Category int

const (
	// CategoryChild is a proxied child-server tool.
	CategoryChild Category = iota
	// CategoryResourceBridge is a synthetic resource-read / resource-templates
	// tool.
	CategoryResourceBridge
	// CategoryMeta is a moxy/framework builtin (no server prefix). Builtins
	// bypass the template entirely and are never placed in a Registry; the
	// value exists for completeness.
	CategoryMeta
)

// Entry is the canonical identity behind one rendered name. Original is the
// child's own tool/prompt name — exactly what the child receives on dispatch,
// preserved verbatim so downstream routing (the "status", "resource-read" and
// "resource-templates" special cases) never has to re-parse the rendered name.
type Entry struct {
	Server   string
	Original string
	Kind     Kind
	Category Category
}

// Template is a parsed, validated name template. The zero value is not usable;
// construct via Parse or DefaultTemplate. Templates are immutable and safe to
// copy by value (the precomputed inverse regexp is shared).
type Template struct {
	raw      string
	segments []segment
	hasTool  bool
	// inverse is a precomputed anchored regexp for structural reverse resolution
	// (the moxy render --resolve CLI). nil when the template is not invertible.
	inverse *regexp.Regexp
	// serverGroup / toolGroup are 1-based submatch indices into inverse, or 0
	// when that placeholder is absent.
	serverGroup int
	toolGroup   int
}

type segment struct {
	literal     string // when kind == segLiteral
	placeholder placeholderKind
}

type placeholderKind int

const (
	segLiteral placeholderKind = iota
	segServer
	segTool
)

// DefaultTemplate is the historical "{server}.{tool}" dot join. It never errors.
func DefaultTemplate() Template {
	t, err := Parse("")
	if err != nil { // unreachable; the default template is always valid
		panic("naming: default template failed to parse: " + err.Error())
	}
	return t
}

// Parse validates a template string. The empty string yields the default
// "{server}.{tool}". Errors on an unknown placeholder, an unterminated brace, or
// a template with no {tool} placeholder.
func Parse(raw string) (Template, error) {
	spec := raw
	if spec == "" {
		spec = "{server}.{tool}"
	}

	var (
		segs        []segment
		hasServer   bool
		hasTool     bool
		serverCount int
		toolCount   int
		lit         strings.Builder
	)
	flushLiteral := func() {
		if lit.Len() > 0 {
			segs = append(segs, segment{literal: lit.String(), placeholder: segLiteral})
			lit.Reset()
		}
	}

	for i := 0; i < len(spec); {
		c := spec[i]
		if c != '{' {
			lit.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(spec[i:], '}')
		if end < 0 {
			return Template{}, fmt.Errorf("unterminated '{' in template %q", raw)
		}
		name := spec[i+1 : i+end]
		switch name {
		case "server":
			flushLiteral()
			segs = append(segs, segment{placeholder: segServer})
			hasServer = true
			serverCount++
		case "tool":
			flushLiteral()
			segs = append(segs, segment{placeholder: segTool})
			hasTool = true
			toolCount++
		default:
			return Template{}, fmt.Errorf(
				"unknown placeholder {%s} in template %q (valid: {server}, {tool})",
				name, raw,
			)
		}
		i += end + 1
	}
	flushLiteral()

	if !hasTool {
		return Template{}, fmt.Errorf("template %q must contain {tool}", raw)
	}

	t := Template{raw: spec, segments: segs, hasTool: hasTool}
	t.buildInverse(hasServer, serverCount, toolCount)
	return t, nil
}

// Render produces the advertised name for one (server, tool) pair by pure
// substitution. The zero-value Template (never Parsed) renders as the default
// "{server}.{tool}" dot join, so a Template field left unset is safe to use.
func (t Template) Render(server, tool string) string {
	if len(t.segments) == 0 {
		return server + "." + tool
	}
	var b strings.Builder
	for _, s := range t.segments {
		switch s.placeholder {
		case segLiteral:
			b.WriteString(s.literal)
		case segServer:
			b.WriteString(server)
		case segTool:
			b.WriteString(tool)
		}
	}
	return b.String()
}

// String returns the canonical template text (the default expanded form when
// the input was empty), suitable for logging.
func (t Template) String() string { return t.raw }

// IsDefault reports whether this is the historical "{server}.{tool}" template
// (including the zero value, which renders the same way). The proxy keeps the
// splitPrefix dispatch fast path (and statsd/category parsing) for the default,
// engaging the registry only under a custom template.
func (t Template) IsDefault() bool { return t.raw == "" || t.raw == "{server}.{tool}" }

// Invertible reports whether Resolve can structurally recover (server, tool)
// from a rendered name without the live server set. True only when the template
// has exactly one {server} and one {tool} separated by a non-empty literal.
func (t Template) Invertible() bool { return t.inverse != nil }

// Resolve attempts a best-effort structural inverse of a rendered name. It is
// used only by the `moxy render --resolve` CLI; the proxy always uses an exact
// Registry instead. Returns ok=false when the template is not Invertible or the
// rendered string does not match its shape.
func (t Template) Resolve(rendered string) (server, tool string, ok bool) {
	if t.inverse == nil {
		return "", "", false
	}
	m := t.inverse.FindStringSubmatch(rendered)
	if m == nil {
		return "", "", false
	}
	if t.serverGroup > 0 {
		server = m[t.serverGroup]
	}
	if t.toolGroup > 0 {
		tool = m[t.toolGroup]
	}
	return server, tool, true
}

// buildInverse precomputes the structural-inverse regexp when the template shape
// permits it (see Invertible). Non-invertible templates leave inverse nil.
func (t *Template) buildInverse(hasServer bool, serverCount, toolCount int) {
	if !hasServer || serverCount != 1 || toolCount != 1 {
		return
	}
	// Require a non-empty literal between the two placeholders so the split is
	// anchored.
	var (
		pat        strings.Builder
		group      int
		seenServer bool
		seenTool   bool
		literalGap bool
	)
	pat.WriteString("^")
	for i, s := range t.segments {
		switch s.placeholder {
		case segLiteral:
			pat.WriteString(regexp.QuoteMeta(s.literal))
			if seenServer != seenTool { // exactly one placeholder seen so far
				literalGap = true
			}
		case segServer, segTool:
			group++
			// The placeholder appearing later is greedy so a separator that
			// recurs inside the value is consumed by the later field.
			last := i == lastPlaceholderIndex(t.segments)
			if last {
				pat.WriteString("(.+)")
			} else {
				pat.WriteString("(.+?)")
			}
			if s.placeholder == segServer {
				t.serverGroup = group
				seenServer = true
			} else {
				t.toolGroup = group
				seenTool = true
			}
		}
	}
	pat.WriteString("$")
	if !literalGap {
		// Placeholders are adjacent (e.g. "{server}{tool}"); not invertible.
		t.serverGroup, t.toolGroup = 0, 0
		return
	}
	t.inverse = regexp.MustCompile(pat.String())
}

func lastPlaceholderIndex(segs []segment) int {
	last := -1
	for i, s := range segs {
		if s.placeholder != segLiteral {
			last = i
		}
	}
	return last
}

// CollisionError reports that two distinct (server, tool) pairs render to the
// same name under a template.
type CollisionError struct {
	Template string
	Rendered string
	A, B     Entry
}

func (e *CollisionError) Error() string {
	return fmt.Sprintf(
		"template %q: rendered name %q produced by both (%s, %s) and (%s, %s)",
		e.Template, e.Rendered,
		e.A.Server, e.A.Original,
		e.B.Server, e.B.Original,
	)
}

// Registry maps a rendered name to its canonical Entry for a single Kind. It is
// immutable once built.
type Registry struct {
	m map[string]Entry
}

// Lookup returns the Entry for a rendered name.
func (r Registry) Lookup(rendered string) (Entry, bool) {
	e, ok := r.m[rendered]
	return e, ok
}

// Len reports the number of registered names.
func (r Registry) Len() int { return len(r.m) }

// Builder accumulates entries in insertion order, then renders and collision-
// checks them in one shot at Build. Insertion order makes the CollisionError
// deterministic across runs.
type Builder struct {
	tmpl    Template
	entries []Entry
}

// NewBuilder starts a Builder for the given template.
func NewBuilder(t Template) *Builder { return &Builder{tmpl: t} }

// Add appends one entry. The rendered name is computed at Build time.
func (b *Builder) Add(e Entry) { b.entries = append(b.entries, e) }

// Build renders every entry into a first-wins Registry and returns it together
// with a *CollisionError for the first time two pairs render to the same name
// (nil when there is none). The Registry is always usable: a collision keeps the
// earlier-Added entry and drops the later one, so callers can both serve the map
// and decide whether to treat the collision as fatal (startup) or degrade-and-
// log (a live reload). Entries are processed in Add order, so the error is
// deterministic across runs.
func (b *Builder) Build() (Registry, error) {
	m := make(map[string]Entry, len(b.entries))
	var firstErr error
	for _, e := range b.entries {
		name := b.tmpl.Render(e.Server, e.Original)
		if prev, ok := m[name]; ok {
			if firstErr == nil {
				firstErr = &CollisionError{
					Template: b.tmpl.String(),
					Rendered: name,
					A:        prev,
					B:        e,
				}
			}
			continue // first-wins: keep prev, drop this one
		}
		m[name] = e
	}
	return Registry{m: m}, firstErr
}
