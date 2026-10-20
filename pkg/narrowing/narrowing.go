// Package narrowing decides whether one effective moxyfile grants no more
// than another. A spawner that hands a child its own toolset must not hand it
// more than it holds (clown FDR 0019 §5, spinclass FDR 0032 D13); Check is the
// relation that makes "no more than" precise, key by key. moxyfile(5)
// NARROWING is the user-facing statement of the same rules.
//
// Both sides must be effective configs: cascade merged, includes resolved,
// loaded without $VAR expansion (moxyfile.Load*Literal). Check never reads the
// environment, so it compares what was written, not what it expands to.
package narrowing

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"code.linenisgreat.com/moxy/pkg/moxyfile"
)

// Violation is one way the child grants more than the parent.
type Violation struct {
	// Key is the moxyfile key path the violation is about, e.g.
	// "servers.smith" or "disable-moxins".
	Key string `json:"key"`
	// Subject names the server, moxin or tool at fault; empty when the key
	// alone identifies it (e.g. "builtin-native").
	Subject string `json:"subject"`
	Message string `json:"message"`
}

func (v Violation) String() string {
	return v.Key + ": " + v.Message
}

// Check returns every way child grants more than parent, sorted by key, then
// subject, then message. An empty result means child narrows parent.
func Check(parent, child moxyfile.Config) []Violation {
	var vs []Violation
	vs = append(vs, checkBuiltinNative(parent, child)...)
	vs = append(vs, checkDisableMoxins(parent, child)...)
	vs = append(vs, checkAllowTools(parent, child)...)
	vs = append(vs, checkServers(parent, child)...)
	vs = append(vs, checkCredentials(parent, child)...)
	vs = append(vs, checkResolvedMoxins(parent, child)...)
	sortViolations(vs)
	return vs
}

// sortViolations sorts in place by key, then subject, then message — the
// order both Check and CheckReload report in.
func sortViolations(vs []Violation) {
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Message < b.Message
	})
}

// checkBuiltinNative: the cascade lets the closest file re-enable the system
// moxins; a child may not, because the parent never had them.
func checkBuiltinNative(parent, child moxyfile.Config) []Violation {
	parentOff := parent.BuiltinNative != nil && !*parent.BuiltinNative
	childOff := child.BuiltinNative != nil && !*child.BuiltinNative
	if parentOff && !childOff {
		return []Violation{{
			Key:     "builtin-native",
			Message: "parent sets false, child does not",
		}}
	}
	return nil
}

func checkDisableMoxins(parent, child moxyfile.Config) []Violation {
	childDisables := stringSet(child.DisableMoxins)
	var vs []Violation
	for entry := range stringSet(parent.DisableMoxins) {
		if !moxyfile.CoveredBy(entry, childDisables) {
			vs = append(vs, Violation{
				Key:     "disable-moxins",
				Subject: entry,
				Message: fmt.Sprintf("parent disables %s, child does not", entry),
			})
		}
	}
	return vs
}

// checkAllowTools judges the two tool levels, use (allow-tools) and pass
// (pass-tools), below which lies none. An allowlist is a restriction, so once
// the parent has one the child must have one too. Every tool the child holds,
// at either level, must be one the parent holds at some level: a pass holder
// may confer use on its child, which is the one upward step the relation
// permits (spinclass FDR 0032 D11). With no parent allowlist the parent holds
// every tool at use, and the child may set any lists it likes.
func checkAllowTools(parent, child moxyfile.Config) []Violation {
	if parent.AllowTools == nil {
		return nil
	}
	if child.AllowTools == nil {
		return []Violation{{
			Key:     "allow-tools",
			Message: "parent sets an allowlist, child does not",
		}}
	}
	parentHolds := stringSet(parent.AllowTools)
	for entry := range stringSet(parent.PassTools) {
		parentHolds[entry] = true
	}
	var vs []Violation
	for _, level := range []struct {
		key, name string
		entries   []string
	}{
		{"allow-tools", "use", child.AllowTools},
		{"pass-tools", "pass", child.PassTools},
	} {
		for entry := range stringSet(level.entries) {
			if !moxyfile.CoveredBy(entry, parentHolds) {
				vs = append(vs, Violation{
					Key:     level.key,
					Subject: entry,
					Message: fmt.Sprintf("child holds %s at %s, parent at none", entry, level.name),
				})
			}
		}
	}
	return vs
}

// CheckReload is Check extended with moxy#461's bound on pass: a tool the
// parent reference holds at pass may reach child's allow-tools (use) only if
// it was already there when this serving process started — the one
// deliberate upward step Check permits (a pass holder's own parent
// pre-approving conferral) is not a license for the holder to promote itself
// via a reload.
//
// startupPinned is false for the very first check a process ever makes —
// genuine startup, with no prior baseline to compare against; that first
// check's own result becomes the baseline every later one (a `restart`
// reload, full or single-moxin) is judged against, and startup is that
// baseline's allow-tools set. A nil startup (the process started with
// allow-tools itself unset) covers every tool, per AllowToolSet's own
// nil-means-unrestricted rule — not "covers none".
func CheckReload(
	parent, child moxyfile.Config,
	startupPinned bool,
	startup moxyfile.AllowToolSet,
) []Violation {
	vs := Check(parent, child)
	vs = append(vs, checkPassNotPromoted(parent, child, startupPinned, startup)...)
	sortViolations(vs)
	return vs
}

func checkPassNotPromoted(
	parent, child moxyfile.Config,
	startupPinned bool,
	startup moxyfile.AllowToolSet,
) []Violation {
	if !startupPinned || startup == nil {
		return nil
	}
	parentAllow := stringSet(parent.AllowTools)
	parentPass := stringSet(parent.PassTools)
	var vs []Violation
	// Iterating child's own allow-tools (not parent's pass-tools) is what
	// correctly handles a bare pass name promoted via a dotted sub-tool: a
	// parent passing "smith" whole, with the child's reload holding only
	// "smith.issue-close" at use, must still be caught (entry="smith.issue-close"
	// is CoveredBy the bare "smith" in parentPass) — the reverse direction
	// would ask whether CoveredBy("smith", {"smith.issue-close"}), which is
	// false, and miss it entirely.
	for entry := range stringSet(child.AllowTools) {
		if moxyfile.CoveredBy(entry, parentAllow) {
			continue // directly granted at use by the parent; not a pass promotion
		}
		if !moxyfile.CoveredBy(entry, parentPass) {
			continue // not something the parent passes either
		}
		if moxyfile.CoveredBy(entry, startup) {
			continue // already at use when the process started
		}
		vs = append(vs, Violation{
			Key:     "allow-tools",
			Subject: entry,
			Message: "held by the parent at pass; not at use when this process started, so a reload cannot raise it (moxy#461)",
		})
	}
	return vs
}

// checkResolvedMoxins closes what the moxyfile keys cannot say: which moxin
// directories are in play. A moxin's tools and their permission tiers are
// read from its directory, so a child whose MOXIN_PATH resolves a name to a
// different directory — or surfaces a moxin the parent never found — holds
// something the parent did not, however well its moxyfile narrows.
//
// It applies only when both sides carry a record. A serving moxy always
// records what it discovered; a parent reference carries one when it was
// written by `show-effective-moxyfile`. A moxin the child cannot reach anyway
// — one it disables, or one a [[servers]] entry shadows — is not held against
// it.
func checkResolvedMoxins(parent, child moxyfile.Config) []Violation {
	if parent.ResolvedMoxins == nil || child.ResolvedMoxins == nil {
		return nil
	}
	unreachable := stringSet(child.DisableMoxins)
	for _, srv := range child.Servers {
		unreachable[srv.Name] = true
	}

	var vs []Violation
	for name, source := range child.ResolvedMoxins {
		if unreachable[name] {
			continue
		}
		parentSource, inParent := parent.ResolvedMoxins[name]
		switch {
		case !inParent:
			vs = append(vs, Violation{
				Key:     "resolved-moxins." + name,
				Subject: name,
				Message: fmt.Sprintf("moxin from %s, not in parent", source),
			})
		case parentSource != source:
			vs = append(vs, Violation{
				Key:     "resolved-moxins." + name,
				Subject: name,
				Message: fmt.Sprintf("moxin from %s, parent's is from %s", source, parentSource),
			})
		}
	}
	return vs
}

func checkServers(parent, child moxyfile.Config) []Violation {
	parentServers := serversByName(parent)
	childServers := serversByName(child)
	parentDisabled := stringSet(parent.DisableServers)
	childDisabled := stringSet(child.DisableServers)
	childDisabledMoxins := stringSet(child.DisableMoxins)

	var vs []Violation
	for name, srv := range childServers {
		if childDisabled[name] {
			continue
		}
		key := "servers." + name
		parentSrv, inParent := parentServers[name]
		switch {
		case !inParent:
			vs = append(vs, Violation{Key: key, Subject: name, Message: "not in parent"})
		case parentDisabled[name]:
			vs = append(vs, Violation{Key: key, Subject: name, Message: "disabled in parent"})
		default:
			for _, msg := range invocationDifferences(parentSrv, srv) {
				vs = append(vs, Violation{Key: key, Subject: name, Message: msg})
			}
		}
	}

	// A [[servers]] entry shadows the moxin of the same name, even while the
	// server is disabled. Dropping the entry would surface that moxin, which
	// the parent could never reach.
	for name := range parentServers {
		if _, defined := childServers[name]; defined || childDisabledMoxins[name] {
			continue
		}
		vs = append(vs, Violation{
			Key:     "disable-moxins",
			Subject: name,
			Message: fmt.Sprintf(
				"parent server %s shadows the moxin of that name; child neither defines the server nor disables the moxin",
				name,
			),
		})
	}
	return vs
}

// invocationDifferences lists the fields in which child would run something
// other than what parent runs. Fields that only change how moxy presents the
// same server (ephemeral, paginate, progressive-disclosure,
// generate-resource-tools) are not rights and are not compared.
func invocationDifferences(parent, child moxyfile.ServerConfig) []string {
	var diffs []string
	differs := func(field string, same bool) {
		if !same {
			diffs = append(diffs, field+" differs from parent")
		}
	}
	differs("command", slices.Equal(parent.Command.Parts(), child.Command.Parts()))
	differs("url", parent.URL == child.URL)
	differs("headers", maps.Equal(parent.Headers, child.Headers))
	differs("headers-helper", sameOptional(parent.HeadersHelper, child.HeadersHelper))
	differs("oauth", sameOptional(parent.OAuth, child.OAuth))
	differs("nix-devshell", sameOptional(parent.NixDevshell, child.NixDevshell))

	// The same text can still run something else if it is resolved against
	// the child's working directory, which the parent does not control.
	if exe := child.Command.Parts(); len(exe) > 0 && isWorkingDirRelative(exe[0]) {
		diffs = append(diffs, "command is relative to the working directory")
	}
	if child.HeadersHelper != nil && commandIsWorkingDirRelative(*child.HeadersHelper) {
		diffs = append(diffs, "headers-helper is relative to the working directory")
	}
	if child.NixDevshell != nil && flakeRefIsWorkingDirRelative(*child.NixDevshell) {
		diffs = append(diffs, "nix-devshell is relative to the working directory")
	}

	// An annotation filter passes a tool that matches ANY of its hints, so
	// every hint adds tools. With no parent filter the child may filter as it
	// likes. With one, the child must filter too, on a subset of the parent's
	// hints: no hint of its own, and none with a different value.
	if parent.Annotations == nil {
		return diffs
	}
	if child.Annotations == nil {
		return append(diffs, "annotations: parent filters tools, child does not")
	}
	p, c := *parent.Annotations, *child.Annotations
	for _, hint := range []struct {
		name          string
		parent, child *bool
	}{
		{"readOnlyHint", p.ReadOnlyHint, c.ReadOnlyHint},
		{"destructiveHint", p.DestructiveHint, c.DestructiveHint},
		{"idempotentHint", p.IdempotentHint, c.IdempotentHint},
		{"openWorldHint", p.OpenWorldHint, c.OpenWorldHint},
	} {
		if hint.child != nil && !sameOptional(hint.parent, hint.child) {
			diffs = append(diffs, fmt.Sprintf(
				"annotations.%s = %t in child, not in parent's filter", hint.name, *hint.child,
			))
		}
	}
	return diffs
}

// isWorkingDirRelative reports whether an executable is named by a path that
// is resolved against the working directory: it has a directory part and is
// neither absolute nor anchored at ~/ or a $VAR. A bare name is looked up on
// PATH, which is the launcher's to control, not this relation's.
func isWorkingDirRelative(exe string) bool {
	if !strings.Contains(exe, "/") {
		return false
	}
	return !strings.HasPrefix(exe, "/") && !strings.HasPrefix(exe, "~/") && !strings.HasPrefix(exe, "$")
}

// commandIsWorkingDirRelative applies isWorkingDirRelative to the executable
// of a whitespace-split command string (headers-helper, credentials).
func commandIsWorkingDirRelative(command string) bool {
	fields := strings.Fields(command)
	return len(fields) > 0 && isWorkingDirRelative(fields[0])
}

// flakeRefIsWorkingDirRelative reports whether nix would resolve a flake
// reference against the working directory. A path-like scheme must carry an
// absolute or ~/ path; with no scheme, "." and ".." forms and anything with a
// directory part are paths. A bare word ("nixpkgs") is a registry id and any
// other scheme ("github:org/repo") is not a local path.
func flakeRefIsWorkingDirRelative(ref string) bool {
	for _, scheme := range []string{"path:", "git+file:", "file:"} {
		if rest, ok := strings.CutPrefix(ref, scheme); ok {
			rest = strings.TrimPrefix(rest, "//")
			return !strings.HasPrefix(rest, "/") && !strings.HasPrefix(rest, "~/")
		}
	}
	if strings.Contains(ref, ":") {
		return false
	}
	if strings.HasPrefix(ref, ".") {
		return true
	}
	return strings.Contains(ref, "/") && !strings.HasPrefix(ref, "/") && !strings.HasPrefix(ref, "~/")
}

func checkCredentials(parent, child moxyfile.Config) []Violation {
	if child.Credentials == nil {
		return nil
	}
	if parent.Credentials == nil {
		return []Violation{{Key: "credentials", Message: "not in parent"}}
	}
	if *parent.Credentials != *child.Credentials {
		return []Violation{{Key: "credentials", Message: "differs from parent"}}
	}
	// These commands are executed too; identical text is not the same
	// program when it is resolved against the child's working directory.
	var vs []Violation
	for _, c := range []struct{ name, command string }{
		{"read", child.Credentials.Read},
		{"write", child.Credentials.Write},
		{"delete", child.Credentials.Delete},
	} {
		if commandIsWorkingDirRelative(c.command) {
			vs = append(vs, Violation{
				Key:     "credentials",
				Subject: c.name,
				Message: c.name + " is relative to the working directory",
			})
		}
	}
	return vs
}

func serversByName(cfg moxyfile.Config) map[string]moxyfile.ServerConfig {
	byName := make(map[string]moxyfile.ServerConfig, len(cfg.Servers))
	for _, srv := range cfg.Servers {
		byName[srv.Name] = srv
	}
	return byName
}

func stringSet(entries []string) map[string]bool {
	set := make(map[string]bool, len(entries))
	for _, e := range entries {
		set[e] = true
	}
	return set
}

// sameOptional reports whether two optional values are both unset or both
// set to the same thing.
func sameOptional[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
