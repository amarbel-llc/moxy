package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// CheckPermission checks whether a file path is allowed by the permission rules.
//
// Composition:
//   - No rules at all → permit (backward compatible)
//   - Any deny rule matches → denied (deny always wins over allow)
//   - Any allow rules exist → path must match at least one
func (pc *PermissionConfig) CheckPermission(absPath string) error {
	if pc == nil || (len(pc.Allow) == 0 && len(pc.Deny) == 0) {
		return nil
	}

	// Check deny rules first — deny always wins.
	for _, rule := range pc.Deny {
		if matchPathRule(rule, absPath) {
			return fmt.Errorf("access denied: %q matches deny rule", absPath)
		}
	}

	// If no allow rules exist, permit by default.
	if len(pc.Allow) == 0 {
		return nil
	}

	// Must match at least one allow rule.
	for _, rule := range pc.Allow {
		if matchPathRule(rule, absPath) {
			return nil
		}
	}

	return fmt.Errorf("access denied: %q — no allow rule matches", absPath)
}

func matchPathRule(rule PathRule, absPath string) bool {
	for _, pattern := range rule.Path {
		if matchPattern(pattern, absPath) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, absPath string) bool {
	// Try exact filepath.Match first (supports *, ?, []).
	if matched, _ := filepath.Match(pattern, absPath); matched {
		return true
	}

	// Try as a directory prefix: if pattern is a directory path,
	// match anything under it.
	cleanPattern := filepath.Clean(pattern)
	cleanPath := filepath.Clean(absPath)
	if strings.HasPrefix(cleanPath, cleanPattern+string(filepath.Separator)) {
		return true
	}

	// Also match the directory itself.
	if cleanPath == cleanPattern {
		return true
	}

	return false
}
