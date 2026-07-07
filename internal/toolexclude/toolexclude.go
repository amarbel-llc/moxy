// Package toolexclude implements a runtime-mutable, name-based deny-set for
// moxy's tool surface — the dynamic counterpart to the moxyfile's static
// `disable-moxins` key (internal/config/schema).
//
// A Set partitions excluded names into whole-server entries (e.g. "chix",
// blocking every tool that server owns) and per-tool entries (e.g.
// "folio.write", blocking just that one), using the same dot-presence rule as
// schema.DisableMoxinSet. Unlike the moxyfile key, a Set is rebuilt from
// scratch on every POST to /clown/exclude-tools (internal/streamhttp) —
// full-replace, not incremental — so the caller always knows the exact
// resulting state.
//
// The proxy consults a Set at both tools/list and tool-call time (mirroring
// internal/toolfilter's category-based deny-set, FDR 0006) so an excluded
// tool is neither advertised nor callable. See
// docs/features/0010-tool-exclude-endpoint.md.
package toolexclude

import "strings"

// Set is a resolved deny-set of excluded server and tool names. The zero
// value excludes nothing.
type Set struct {
	servers map[string]bool // bare moxin/server names, e.g. "chix"
	tools   map[string]bool // dotted rendered tool names, e.g. "folio.write"
}

// Parse partitions a flat list of names into a Set. An entry containing "."
// is treated as a single tool name; anything else is treated as a whole
// server name. Duplicate entries are harmless. A nil or empty slice yields
// the zero Set (excludes nothing).
func Parse(names []string) Set {
	s := Set{
		servers: make(map[string]bool),
		tools:   make(map[string]bool),
	}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if strings.Contains(n, ".") {
			s.tools[n] = true
		} else {
			s.servers[n] = true
		}
	}
	return s
}

// Excludes reports whether the given rendered tool name should be excluded:
// because the exact rendered name is excluded (a per-tool entry, or a
// dotless entry matching a dotless name verbatim — e.g. a builtin like
// "restart" is excluded by its own bare name), or because its owning server
// is excluded wholesale. server may be empty (a builtin has no owning
// server, or the name is unresolvable under a custom template) — an empty
// server never matches a server-name entry.
func (s Set) Excludes(server, renderedName string) bool {
	if s.tools[renderedName] || s.servers[renderedName] {
		return true
	}
	if server == "" {
		return false
	}
	return s.servers[server]
}

// IsEmpty reports whether this Set excludes nothing — callers use it to skip
// filtering work entirely on the common (no exclusions active) path.
func (s Set) IsEmpty() bool {
	return len(s.servers) == 0 && len(s.tools) == 0
}

// Names returns the excluded names as a single flat, sorted-by-insertion-
// unordered list (whole-server entries followed by per-tool entries),
// suitable for the GET /clown/exclude-tools readback. The zero Set returns
// nil.
func (s Set) Names() []string {
	if s.IsEmpty() {
		return nil
	}
	names := make([]string, 0, len(s.servers)+len(s.tools))
	for n := range s.servers {
		names = append(names, n)
	}
	for n := range s.tools {
		names = append(names, n)
	}
	return names
}
