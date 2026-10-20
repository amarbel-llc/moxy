package perms

import "strings"

// Mode says who answers a tool call's permission question at serve time
// (FDR 0014; the enforce mode itself is moxy#486).
type Mode string

const (
	// Delegate is today's behaviour and the default: the call funnel does not
	// resolve a tool's tier; a client-side gate (the Claude Code PreToolUse
	// hook) decides before the call reaches moxy, and a client without one
	// runs every tool it can name.
	Delegate Mode = "delegate"
	// Enforce makes the funnel resolve the tier itself, after the allow-tools
	// gate: Allow runs; Ask, Deny and Unknown are refused with an isError
	// result that says nobody can answer. For clients with no hook in front.
	Enforce Mode = "enforce"
)

// Policy is the permission posture a serving moxy, or an embedder, hands the
// call funnel. The zero value is Delegate, so an unset policy changes
// nothing.
type Policy struct {
	Mode Mode
	// Allow names tools an enforcing funnel runs although they declare no
	// tier (moxyfile(5) enforce-allow): a bare server or moxin name covers
	// all its tools, "server.tool" one. It lifts only Unknown; a declared
	// tier is never overridden. Inert under Delegate.
	Allow []string
}

// Effective reports the mode a zero or explicit Policy stands for.
func (p Policy) Effective() Mode {
	if p.Mode == "" {
		return Delegate
	}
	return p.Mode
}

// Enforcing reports whether the funnel resolves permissions itself.
func (p Policy) Enforcing() bool { return p.Effective() == Enforce }

// Grants reports whether Allow covers the canonical "server.tool" name: the
// exact entry, or the bare server.
func (p Policy) Grants(serverTool string) bool {
	server, _, _ := strings.Cut(serverTool, ".")
	for _, entry := range p.Allow {
		if entry == serverTool || entry == server {
			return true
		}
	}
	return false
}

// Narrower reports whether p grants no more than parent: a child policy may
// move Delegate to Enforce, never Enforce back to Delegate, and under an
// enforcing parent may grant through Allow only what the parent grants. It
// is the rule moxyfile narrowing (FDR 0013) applies to the mode, and the
// rule a per-call policy override is held to.
func (p Policy) Narrower(parent Policy) bool {
	if !parent.Enforcing() {
		return true
	}
	if !p.Enforcing() {
		return false
	}
	for _, entry := range p.Allow {
		if !parent.Grants(entry) {
			return false
		}
	}
	return true
}

// Tighten combines two policies into the stricter one: Enforce if either
// enforces; the Allow lists are joined, since each side's grants were
// written by someone entitled to grant them (narrowing bounds a child's).
func Tighten(a, b Policy) Policy {
	out := Policy{Mode: Delegate}
	if a.Enforcing() || b.Enforcing() {
		out.Mode = Enforce
	}
	seen := map[string]bool{}
	for _, list := range [][]string{a.Allow, b.Allow} {
		for _, entry := range list {
			if !seen[entry] {
				seen[entry] = true
				out.Allow = append(out.Allow, entry)
			}
		}
	}
	return out
}

// ParseMode validates a mode as written in a moxyfile or flag.
func ParseMode(s string) (Mode, bool) {
	switch Mode(s) {
	case "", Delegate:
		return Delegate, true
	case Enforce:
		return Enforce, true
	}
	return "", false
}
