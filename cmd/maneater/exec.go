package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultSessionEnvVar   = "CLAUDE_SESSION_ID"
	defaultSessionFallback = "no-session"
)

// resolveExecSession determines the session string used to namespace cached
// exec results. It reads the env var named by ExecSessionConfig.Env (default
// CLAUDE_SESSION_ID) and falls back to ExecSessionConfig.Fallback (default
// "no-session") when unset or empty. The result is sanitized to a single
// path segment.
func resolveExecSession(ec *ExecConfig) string {
	envName := defaultSessionEnvVar
	fallback := defaultSessionFallback
	if ec != nil && ec.Session != nil {
		if ec.Session.Env != "" {
			envName = ec.Session.Env
		}
		if ec.Session.Fallback != "" {
			fallback = ec.Session.Fallback
		}
	}
	if v := sanitizeSessionSegment(os.Getenv(envName)); v != "" {
		return v
	}
	return sanitizeSessionSegment(fallback)
}

// sanitizeSessionSegment strips any characters outside [A-Za-z0-9._-] so the
// resulting string is safe to use as both a single path segment and a URI
// segment. Returns empty string if nothing usable remains.
func sanitizeSessionSegment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}

type ExecConfig struct {
	Allow   []ExecRule         `toml:"allow"`
	Deny    []ExecRule         `toml:"deny"`
	Session *ExecSessionConfig `toml:"session"`
}

// ExecSessionConfig controls how cached exec result URIs are namespaced.
// The resolved session string becomes the first segment of every result URI
// (maneater.exec://results/{session}/{id}) and the first directory level of
// the on-disk cache layout, so a future cleanup hook can wipe a session's
// results with a single rm -rf.
type ExecSessionConfig struct {
	// Env names the environment variable read at startup to determine the
	// session bucket. Default: "CLAUDE_SESSION_ID".
	Env string `toml:"env"`
	// Fallback is used when the env var is unset or empty. Default:
	// "no-session".
	Fallback string `toml:"fallback"`
}

type ExecRule struct {
	Binary string            `toml:"binary"`
	Args   []string          `toml:"args"`
	Cwd    []string          `toml:"cwd"`
	Env    map[string]string `toml:"env"`
}

// CheckPermission checks whether a command is allowed by the exec permission
// rules. Returns the env vars to inject (from matching allow rules) and an
// error if the command is denied.
//
// Composition:
//   - No rules at all → permit (backward compatible)
//   - Any allow rules exist → command must match at least one
//   - Any deny rule matches → denied (deny always wins over allow)
func (ec *ExecConfig) CheckPermission(
	command string,
	cwd string,
	env map[string]string,
) (injectedEnv map[string]string, err error) {
	if ec == nil || (len(ec.Allow) == 0 && len(ec.Deny) == 0) {
		return nil, nil
	}

	binary, args := parseCommand(command)
	if binary == "" {
		return nil, fmt.Errorf("exec denied: unable to parse command %q", command)
	}

	// Check deny rules first — deny always wins.
	for _, rule := range ec.Deny {
		if matchRule(rule, binary, args, cwd, env) {
			return nil, fmt.Errorf(
				"exec denied: %q matches deny rule for %s",
				command, formatRule(rule),
			)
		}
	}

	// If no allow rules exist, permit by default.
	if len(ec.Allow) == 0 {
		return nil, nil
	}

	// Must match at least one allow rule.
	for _, rule := range ec.Allow {
		if matchRule(rule, binary, args, cwd, env) {
			return rule.Env, nil
		}
	}

	return nil, fmt.Errorf(
		"exec denied: %q — no allow rule matches binary %q",
		command, binary,
	)
}

// parseCommand extracts the binary name and arguments from a shell command
// string. Skips leading VAR=value environment assignments. Returns the
// basename of the binary (e.g. "/usr/bin/git" → "git").
func parseCommand(command string) (binary string, args []string) {
	fields := strings.Fields(command)

	// Skip VAR=value prefixes.
	i := 0
	for i < len(fields) && strings.Contains(fields[i], "=") {
		i++
	}

	if i >= len(fields) {
		return "", nil
	}

	binary = filepath.Base(fields[i])
	args = fields[i+1:]
	return binary, args
}

// matchRule checks whether a parsed command matches a single rule.
func matchRule(
	rule ExecRule,
	binary string,
	args []string,
	cwd string,
	env map[string]string,
) bool {
	if rule.Binary != binary {
		return false
	}

	if len(rule.Args) > 0 && !matchArgs(rule.Args, args) {
		return false
	}

	if len(rule.Cwd) > 0 && !matchCwd(rule.Cwd, cwd) {
		return false
	}

	return true
}

// matchArgs checks whether the command's args match any of the rule's arg
// patterns. Each pattern is split on whitespace and matched as a prefix
// against the command args.
func matchArgs(patterns []string, args []string) bool {
	for _, pattern := range patterns {
		patternFields := strings.Fields(pattern)
		if len(patternFields) > len(args) {
			continue
		}
		match := true
		for j, pf := range patternFields {
			if args[j] != pf {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// matchCwd checks whether the command's working directory matches any of the
// rule's cwd glob patterns.
func matchCwd(patterns []string, cwd string) bool {
	if cwd == "" {
		return false
	}
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, cwd); matched {
			return true
		}
	}
	return false
}

func formatRule(rule ExecRule) string {
	s := rule.Binary
	if len(rule.Args) > 0 {
		s += " [" + strings.Join(rule.Args, ", ") + "]"
	}
	return s
}
