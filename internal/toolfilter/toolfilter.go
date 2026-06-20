// Package toolfilter resolves a `serve-http --expose` selector into a set of
// allowed tool categories and classifies tool names into those categories.
//
// A category is one of child (proxied child-server tools), resource-bridge
// (the synthetic <server>.resource-read / .resource-templates tools), or meta
// (moxy/framework builtins like restart, batch, async, status). The proxy
// consults a resolved Filter at both tools/list and tool-call time so that an
// excluded category is neither advertised nor callable. See
// docs/features/0006-tool-exposure-filter.md.
//
// Internally a Filter is a deny-set, so the zero value blocks nothing and
// exposes everything — a Proxy with an unset filter behaves exactly as it did
// before --expose existed.
package toolfilter

import (
	"fmt"
	"strings"
)

// Category is the exposure class a tool belongs to. Categorize maps a tool
// name to exactly one Category.
type Category int

const (
	// Child is a proxied child-server tool, e.g. grit.status.
	Child Category = iota
	// ResourceBridge is a synthetic <server>.resource-read or
	// <server>.resource-templates tool.
	ResourceBridge
	// Meta is a moxy or framework builtin (no server prefix), e.g. restart.
	Meta
	numCategories
)

func (c Category) String() string {
	switch c {
	case Child:
		return "child"
	case ResourceBridge:
		return "resource-bridge"
	case Meta:
		return "meta"
	default:
		return "unknown"
	}
}

// Filter is a resolved set of allowed categories. The zero value allows every
// category (blocks nothing), so an unset filter exposes the full tool surface.
type Filter struct {
	block [numCategories]bool
}

// All allows every category — the default when --expose is unset and the only
// filter used by serve-mcp. It is the zero value.
func All() Filter {
	return Filter{}
}

// Allows reports whether tools in category c should be exposed.
func (f Filter) Allows(c Category) bool {
	if c < 0 || c >= numCategories {
		return false
	}
	return !f.block[c]
}

// IsAll reports whether every category is allowed — the default, no-op filter.
// Callers use it to skip filtering work entirely on the common path.
func (f Filter) IsAll() bool {
	return f == Filter{}
}

// String renders the allowed categories for logging, e.g. "child,resource-bridge".
func (f Filter) String() string {
	var on []string
	for c := Category(0); c < numCategories; c++ {
		if f.Allows(c) {
			on = append(on, c.String())
		}
	}
	if len(on) == 0 {
		return "none"
	}
	return strings.Join(on, ",")
}

// Categorize maps a fully-namespaced tool name to its Category. A name with no
// dot separator is a builtin (Meta); a <server>.resource-read or
// <server>.resource-templates name is ResourceBridge; anything else is Child.
func Categorize(name string) Category {
	_, sub, ok := strings.Cut(name, ".")
	if !ok {
		return Meta
	}
	if sub == "resource-read" || sub == "resource-templates" {
		return ResourceBridge
	}
	return Child
}

// Parse resolves a --expose selector spec into a Filter. The empty spec yields
// All(). Selectors are comma-separated and applied left-to-right over a base of
// All(): a profile name (full, no-meta, resources-only) resets the working set,
// a +cat/-cat toggle adjusts it. Unknown profile or category names are errors.
func Parse(spec string) (Filter, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return All(), nil
	}
	f := All()
	for _, raw := range strings.Split(spec, ",") {
		sel := strings.TrimSpace(raw)
		if sel == "" {
			continue
		}
		if sel[0] == '+' || sel[0] == '-' {
			cat, err := parseCategory(sel[1:])
			if err != nil {
				return Filter{}, err
			}
			f.block[cat] = sel[0] == '-'
			continue
		}
		p, err := parseProfile(sel)
		if err != nil {
			return Filter{}, err
		}
		f = p
	}
	return f, nil
}

func parseProfile(name string) (Filter, error) {
	switch name {
	case "full":
		return All(), nil
	case "no-meta":
		return Filter{block: [numCategories]bool{Meta: true}}, nil
	case "resources-only":
		return Filter{block: [numCategories]bool{Child: true, ResourceBridge: true, Meta: true}}, nil
	default:
		return Filter{}, fmt.Errorf(
			"unknown --expose profile %q (valid profiles: full, no-meta, resources-only; or +/-{child,resource-bridge,meta})",
			name,
		)
	}
}

func parseCategory(name string) (Category, error) {
	switch name {
	case "child":
		return Child, nil
	case "resource-bridge":
		return ResourceBridge, nil
	case "meta":
		return Meta, nil
	default:
		return 0, fmt.Errorf(
			"unknown --expose category %q (valid categories: child, resource-bridge, meta)",
			name,
		)
	}
}
