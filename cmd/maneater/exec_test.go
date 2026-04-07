package main

import (
	"testing"
)

func TestSanitizeSessionSegment(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"abc-123", "abc-123"},
		{"sess.1_a", "sess.1_a"},
		{"with/slash", "withslash"},
		{"with space", "withspace"},
		{"a/b/c", "abc"},
		{"!!!", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := sanitizeSessionSegment(tt.in)
		if got != tt.want {
			t.Errorf("sanitizeSessionSegment(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolveExecSession(t *testing.T) {
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("MY_CUSTOM_SESSION", "")

	// Default fallback when env unset and no config.
	if got := resolveExecSession(nil); got != "no-session" {
		t.Errorf("nil cfg, no env: got %q, want %q", got, "no-session")
	}

	// Default env var when set.
	t.Setenv("CLAUDE_SESSION_ID", "abc-123")
	if got := resolveExecSession(nil); got != "abc-123" {
		t.Errorf("CLAUDE_SESSION_ID set: got %q, want %q", got, "abc-123")
	}

	// Sanitization of env var value.
	t.Setenv("CLAUDE_SESSION_ID", "abc/def 123")
	if got := resolveExecSession(nil); got != "abcdef123" {
		t.Errorf("dirty env: got %q, want %q", got, "abcdef123")
	}

	// Custom env var name from config.
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("MY_CUSTOM_SESSION", "custom")
	cfg := &ExecConfig{Session: &ExecSessionConfig{Env: "MY_CUSTOM_SESSION"}}
	if got := resolveExecSession(cfg); got != "custom" {
		t.Errorf("custom env: got %q, want %q", got, "custom")
	}

	// Custom fallback.
	t.Setenv("MY_CUSTOM_SESSION", "")
	cfg = &ExecConfig{Session: &ExecSessionConfig{Env: "MY_CUSTOM_SESSION", Fallback: "orphan"}}
	if got := resolveExecSession(cfg); got != "orphan" {
		t.Errorf("custom fallback: got %q, want %q", got, "orphan")
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		wantBinary string
		wantArgs   []string
	}{
		{
			name:       "simple",
			command:    "git status",
			wantBinary: "git",
			wantArgs:   []string{"status"},
		},
		{
			name:       "absolute_path",
			command:    "/usr/bin/git log --oneline",
			wantBinary: "git",
			wantArgs:   []string{"log", "--oneline"},
		},
		{
			name:       "env_prefix",
			command:    "FOO=bar BAZ=qux git push",
			wantBinary: "git",
			wantArgs:   []string{"push"},
		},
		{
			name:       "no_args",
			command:    "ls",
			wantBinary: "ls",
			wantArgs:   nil,
		},
		{
			name:       "empty",
			command:    "",
			wantBinary: "",
			wantArgs:   nil,
		},
		{
			name:       "only_env_vars",
			command:    "FOO=bar",
			wantBinary: "",
			wantArgs:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binary, args := parseCommand(tt.command)
			if binary != tt.wantBinary {
				t.Errorf("binary = %q, want %q", binary, tt.wantBinary)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args len = %d, want %d", len(args), len(tt.wantArgs))
			}
			for i := range args {
				if args[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, args[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestMatchArgs(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		args     []string
		want     bool
	}{
		{
			name:     "exact_match",
			patterns: []string{"status"},
			args:     []string{"status"},
			want:     true,
		},
		{
			name:     "prefix_match",
			patterns: []string{"push --force"},
			args:     []string{"push", "--force", "origin", "master"},
			want:     true,
		},
		{
			name:     "no_match",
			patterns: []string{"push --force"},
			args:     []string{"push", "origin"},
			want:     false,
		},
		{
			name:     "multiple_patterns_one_matches",
			patterns: []string{"status", "diff", "log"},
			args:     []string{"diff", "--cached"},
			want:     true,
		},
		{
			name:     "empty_args",
			patterns: []string{"status"},
			args:     nil,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchArgs(tt.patterns, tt.args)
			if got != tt.want {
				t.Errorf("matchArgs(%v, %v) = %v, want %v", tt.patterns, tt.args, got, tt.want)
			}
		})
	}
}

func TestCheckPermissionNilConfig(t *testing.T) {
	var ec *ExecConfig
	_, err := ec.CheckPermission("git status", "", nil)
	if err == nil {
		t.Error("nil config should deny everything")
	}
}

func TestCheckPermissionNoRules(t *testing.T) {
	ec := &ExecConfig{}
	_, err := ec.CheckPermission("git status", "", nil)
	if err == nil {
		t.Error("empty config should deny everything")
	}
}

func TestCheckPermissionAllowOnly(t *testing.T) {
	ec := &ExecConfig{
		Allow: []ExecRule{
			{Binary: "git", Args: []string{"status", "diff"}},
			{Binary: "jq"},
		},
	}

	// Allowed command.
	_, err := ec.CheckPermission("git status", "", nil)
	if err != nil {
		t.Errorf("git status should be allowed: %v", err)
	}

	// Allowed binary with no args restriction.
	_, err = ec.CheckPermission("jq '.foo'", "", nil)
	if err != nil {
		t.Errorf("jq should be allowed: %v", err)
	}

	// Denied — binary not in allow list.
	_, err = ec.CheckPermission("rm -rf /", "", nil)
	if err == nil {
		t.Error("rm should be denied when allow rules exist")
	}

	// Denied — binary allowed but args don't match.
	_, err = ec.CheckPermission("git push", "", nil)
	if err == nil {
		t.Error("git push should be denied when only status/diff allowed")
	}
}

func TestCheckPermissionDenyWinsOverAllow(t *testing.T) {
	ec := &ExecConfig{
		Allow: []ExecRule{
			{Binary: "git"},
		},
		Deny: []ExecRule{
			{Binary: "git", Args: []string{"push --force"}},
		},
	}

	// Allowed.
	_, err := ec.CheckPermission("git status", "", nil)
	if err != nil {
		t.Errorf("git status should be allowed: %v", err)
	}

	// Denied by specific deny rule.
	_, err = ec.CheckPermission("git push --force origin master", "", nil)
	if err == nil {
		t.Error("git push --force should be denied")
	}
}

func TestCheckPermissionDenyOnlyDeniesEverything(t *testing.T) {
	ec := &ExecConfig{
		Deny: []ExecRule{
			{Binary: "sudo"},
		},
	}

	// Explicitly denied binary.
	_, err := ec.CheckPermission("sudo rm -rf /", "", nil)
	if err == nil {
		t.Error("sudo should be denied")
	}

	// No allow rules → everything denied, not just sudo.
	_, err = ec.CheckPermission("git status", "", nil)
	if err == nil {
		t.Error("git should be denied with no allow rules")
	}
}

func TestCheckPermissionCwdRestriction(t *testing.T) {
	ec := &ExecConfig{
		Allow: []ExecRule{
			{Binary: "git", Cwd: []string{"/home/user/repos/*"}},
		},
	}

	// Matching cwd.
	_, err := ec.CheckPermission("git status", "/home/user/repos/myproject", nil)
	if err != nil {
		t.Errorf("should be allowed in matching cwd: %v", err)
	}

	// Non-matching cwd.
	_, err = ec.CheckPermission("git status", "/tmp", nil)
	if err == nil {
		t.Error("should be denied in non-matching cwd")
	}

	// Empty cwd when cwd restriction set.
	_, err = ec.CheckPermission("git status", "", nil)
	if err == nil {
		t.Error("should be denied when no cwd provided but cwd restriction set")
	}
}

func TestCheckPermissionEnvInjection(t *testing.T) {
	ec := &ExecConfig{
		Allow: []ExecRule{
			{
				Binary: "git",
				Env:    map[string]string{"GIT_AUTHOR_NAME": "bot"},
			},
		},
	}

	env, err := ec.CheckPermission("git commit -m test", "", nil)
	if err != nil {
		t.Fatalf("should be allowed: %v", err)
	}
	if env == nil || env["GIT_AUTHOR_NAME"] != "bot" {
		t.Errorf("expected injected env, got %v", env)
	}
}
