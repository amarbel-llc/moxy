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
	"reflect"
	"sort"

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
	differs("command", reflect.DeepEqual(parent.Command.Parts(), child.Command.Parts()))
	differs("url", parent.URL == child.URL)
	differs("headers", sameHeaders(parent.Headers, child.Headers))
	differs("headers-helper", sameOptionalString(parent.HeadersHelper, child.HeadersHelper))
	differs("oauth", sameOAuth(parent.OAuth, child.OAuth))
	differs("nix-devshell", sameOptionalString(parent.NixDevshell, child.NixDevshell))

	// An annotation filter is a restriction: the child must keep every hint
	// the parent filters on, and may filter on more.
	for _, hint := range annotationHints(parent.Annotations) {
		if kept := annotationHint(child.Annotations, hint.name); kept == nil || *kept != hint.value {
			diffs = append(diffs, fmt.Sprintf(
				"annotations.%s = %t in parent, not kept by child", hint.name, hint.value,
			))
		}
	}
	return diffs
}

type annotationHintValue struct {
	name  string
	value bool
}

func annotationHints(a *config.AnnotationFilter) []annotationHintValue {
	var hints []annotationHintValue
	for _, name := range []string{"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint"} {
		if v := annotationHint(a, name); v != nil {
			hints = append(hints, annotationHintValue{name: name, value: *v})
		}
	}
	return hints
}

func annotationHint(a *config.AnnotationFilter, name string) *bool {
	if a == nil {
		return nil
	}
	switch name {
	case "readOnlyHint":
		return a.ReadOnlyHint
	case "destructiveHint":
		return a.DestructiveHint
	case "idempotentHint":
		return a.IdempotentHint
	case "openWorldHint":
		return a.OpenWorldHint
	}
	return nil
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

func sameHeaders(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if other, ok := b[k]; !ok || other != v {
			return false
		}
	}
	return true
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameOAuth(a, b *config.OAuthConfig) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
