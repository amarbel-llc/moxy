package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

type ExecConfig struct {
	Allow []ExecRule `toml:"allow"`
	Deny  []ExecRule `toml:"deny"`
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
