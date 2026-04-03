package main

import (
	"testing"
)

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
	env, err := ec.CheckPermission("git status", "", nil)
	if err != nil {
		t.Errorf("nil config should allow everything, got error: %v", err)
	}
	if env != nil {
		t.Errorf("nil config should return nil env, got %v", env)
	}
}

func TestCheckPermissionNoRules(t *testing.T) {
	ec := &ExecConfig{}
	_, err := ec.CheckPermission("git status", "", nil)
	if err != nil {
		t.Errorf("empty config should allow everything, got error: %v", err)
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

func TestCheckPermissionDenyOnlyBlocksSpecific(t *testing.T) {
	ec := &ExecConfig{
		Deny: []ExecRule{
			{Binary: "sudo"},
		},
	}

	// Denied binary.
	_, err := ec.CheckPermission("sudo rm -rf /", "", nil)
	if err == nil {
		t.Error("sudo should be denied")
	}

	// No allow rules → everything else allowed.
	_, err = ec.CheckPermission("git status", "", nil)
	if err != nil {
		t.Errorf("git should be allowed with deny-only rules: %v", err)
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
