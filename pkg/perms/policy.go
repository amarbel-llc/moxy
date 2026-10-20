package perms

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

// Narrower reports whether p grants no more than parent: a child policy may
// move Delegate to Enforce, never Enforce back to Delegate. It is the rule
// moxyfile narrowing (FDR 0013) applies to the mode, and the rule a per-call
// policy override is held to.
func (p Policy) Narrower(parent Policy) bool {
	return !parent.Enforcing() || p.Enforcing()
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
