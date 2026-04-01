package config

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

func TestMergeExecRulesAccumulate(t *testing.T) {
	base := Config{
		Exec: &ExecConfig{
			Allow: []ExecRule{{Binary: "git"}},
			Deny:  []ExecRule{{Binary: "sudo"}},
		},
	}
	overlay := Config{
		Exec: &ExecConfig{
			Allow: []ExecRule{{Binary: "jq"}},
			Deny:  []ExecRule{{Binary: "rm"}},
		},
	}

	merged := Merge(base, overlay)

	if merged.Exec == nil {
		t.Fatal("merged exec should not be nil")
	}
	if len(merged.Exec.Allow) != 2 {
		t.Errorf("expected 2 allow rules, got %d", len(merged.Exec.Allow))
	}
	if len(merged.Exec.Deny) != 2 {
		t.Errorf("expected 2 deny rules, got %d", len(merged.Exec.Deny))
	}
}

func TestMergeExecBaseOnlyPreserved(t *testing.T) {
	base := Config{
		Exec: &ExecConfig{
			Allow: []ExecRule{{Binary: "git"}},
		},
	}
	overlay := Config{}

	merged := Merge(base, overlay)
	if merged.Exec == nil || len(merged.Exec.Allow) != 1 {
		t.Error("base exec rules should be preserved when overlay has no exec")
	}
}

func TestMergeExecOverlayOnlyAdded(t *testing.T) {
	base := Config{}
	overlay := Config{
		Exec: &ExecConfig{
			Allow: []ExecRule{{Binary: "git"}},
		},
	}

	merged := Merge(base, overlay)
	if merged.Exec == nil || len(merged.Exec.Allow) != 1 {
		t.Error("overlay exec rules should be added when base has no exec")
	}
}

func TestParseExecConfig(t *testing.T) {
	input := `
[[exec.allow]]
binary = "git"
args = ["status", "diff"]

[[exec.allow]]
binary = "jq"

[[exec.deny]]
binary = "sudo"
`
	cfg, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cfg.Exec == nil {
		t.Fatal("exec config should not be nil")
	}
	if len(cfg.Exec.Allow) != 2 {
		t.Fatalf("expected 2 allow rules, got %d", len(cfg.Exec.Allow))
	}
	if cfg.Exec.Allow[0].Binary != "git" {
		t.Errorf("first allow binary = %q, want git", cfg.Exec.Allow[0].Binary)
	}
	if len(cfg.Exec.Allow[0].Args) != 2 {
		t.Errorf("first allow args len = %d, want 2", len(cfg.Exec.Allow[0].Args))
	}
	if cfg.Exec.Allow[1].Binary != "jq" {
		t.Errorf("second allow binary = %q, want jq", cfg.Exec.Allow[1].Binary)
	}
	if len(cfg.Exec.Deny) != 1 {
		t.Fatalf("expected 1 deny rule, got %d", len(cfg.Exec.Deny))
	}
	if cfg.Exec.Deny[0].Binary != "sudo" {
		t.Errorf("deny binary = %q, want sudo", cfg.Exec.Deny[0].Binary)
	}
}
