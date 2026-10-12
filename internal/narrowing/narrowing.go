// Package narrowing decides whether one effective moxyfile grants no more
// than another. A spawner that hands a child its own toolset must not hand it
// more than it holds (clown FDR 0019 §5, spinclass FDR 0032 D13); Check is the
// relation that makes "no more than" precise, key by key. moxyfile(5)
// NARROWING is the user-facing statement of the same rules.
//
// Both sides must be effective configs: cascade merged, includes resolved,
// loaded without $VAR expansion (config.Load*Literal). Check never reads the
// environment, so it compares what was written, not what it expands to.
package narrowing

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"code.linenisgreat.com/moxy/internal/config"
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
func Check(parent, child config.Config) []Violation {
	var vs []Violation
	vs = append(vs, checkBuiltinNative(parent, child)...)
	vs = append(vs, checkDisableMoxins(parent, child)...)
	vs = append(vs, checkAllowTools(parent, child)...)
	vs = append(vs, checkServers(parent, child)...)
	vs = append(vs, checkCredentials(parent, child)...)
	vs = append(vs, checkResolvedMoxins(parent, child)...)

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
	return vs
}

// checkBuiltinNative: the cascade lets the closest file re-enable the system
// moxins; a child may not, because the parent never had them.
func checkBuiltinNative(parent, child config.Config) []Violation {
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

func checkDisableMoxins(parent, child config.Config) []Violation {
	childDisables := stringSet(child.DisableMoxins)
	var vs []Violation
	for entry := range stringSet(parent.DisableMoxins) {
		if !config.CoveredBy(entry, childDisables) {
			vs = append(vs, Violation{
				Key:     "disable-moxins",
				Subject: entry,
				Message: fmt.Sprintf("parent disables %s, child does not", entry),
			})
		}
	}
	return vs
}

// checkAllowTools: an allowlist is a restriction, so once the parent has one
// the child must have one too, naming nothing the parent's does not cover.
// With no parent allowlist the child may set any list it likes.
func checkAllowTools(parent, child config.Config) []Violation {
	if parent.AllowTools == nil {
		return nil
	}
	if child.AllowTools == nil {
		return []Violation{{
			Key:     "allow-tools",
			Message: "parent sets an allowlist, child does not",
		}}
	}
	parentAllows := stringSet(parent.AllowTools)
	var vs []Violation
	for entry := range stringSet(child.AllowTools) {
		if !config.CoveredBy(entry, parentAllows) {
			vs = append(vs, Violation{
				Key:     "allow-tools",
				Subject: entry,
				Message: fmt.Sprintf("child allows %s, parent does not", entry),
			})
		}
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
func checkResolvedMoxins(parent, child config.Config) []Violation {
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

func checkServers(parent, child config.Config) []Violation {
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
func invocationDifferences(parent, child config.ServerConfig) []string {
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
	if child.NixDevshell != nil && strings.HasPrefix(*child.NixDevshell, ".") {
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

func checkCredentials(parent, child config.Config) []Violation {
	if child.Credentials == nil {
		return nil
	}
	if parent.Credentials == nil {
		return []Violation{{Key: "credentials", Message: "not in parent"}}
	}
	if *parent.Credentials != *child.Credentials {
		return []Violation{{Key: "credentials", Message: "differs from parent"}}
	}
	return nil
}

func serversByName(cfg config.Config) map[string]config.ServerConfig {
	byName := make(map[string]config.ServerConfig, len(cfg.Servers))
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
