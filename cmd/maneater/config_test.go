package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeConfigExecAccumulates(t *testing.T) {
	base := ManeaterConfig{
		Exec: &ExecConfig{
			Allow: []ExecRule{{Binary: "git"}},
			Deny:  []ExecRule{{Binary: "sudo"}},
		},
	}
	overlay := ManeaterConfig{
		Exec: &ExecConfig{
			Allow: []ExecRule{{Binary: "jq"}},
			Deny:  []ExecRule{{Binary: "rm"}},
		},
	}

	merged := MergeConfig(base, overlay)

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

func TestMergeConfigExecBaseOnlyPreserved(t *testing.T) {
	base := ManeaterConfig{
		Exec: &ExecConfig{
			Allow: []ExecRule{{Binary: "git"}},
		},
	}
	overlay := ManeaterConfig{}

	merged := MergeConfig(base, overlay)
	if merged.Exec == nil || len(merged.Exec.Allow) != 1 {
		t.Error("base exec rules should be preserved when overlay has no exec")
	}
}

func TestMergeConfigExecOverlayOnlyAdded(t *testing.T) {
	base := ManeaterConfig{}
	overlay := ManeaterConfig{
		Exec: &ExecConfig{
			Allow: []ExecRule{{Binary: "git"}},
		},
	}

	merged := MergeConfig(base, overlay)
	if merged.Exec == nil || len(merged.Exec.Allow) != 1 {
		t.Error("overlay exec rules should be added when base has no exec")
	}
}

func TestMergeConfigModelsOverlayWins(t *testing.T) {
	base := ManeaterConfig{
		Default: "a",
		Models: map[string]ModelConfig{
			"a": {Path: "/old"},
			"b": {Path: "/b"},
		},
	}
	overlay := ManeaterConfig{
		Default: "c",
		Models: map[string]ModelConfig{
			"a": {Path: "/new"},
			"c": {Path: "/c"},
		},
	}

	merged := MergeConfig(base, overlay)
	if merged.Default != "c" {
		t.Errorf("Default = %q, want %q", merged.Default, "c")
	}
	if merged.Models["a"].Path != "/new" {
		t.Errorf("Model a path = %q, want /new", merged.Models["a"].Path)
	}
	if merged.Models["b"].Path != "/b" {
		t.Errorf("Model b should be preserved from base")
	}
	if merged.Models["c"].Path != "/c" {
		t.Errorf("Model c should be added from overlay")
	}
}

func TestLoadManeaterHierarchyMergesExec(t *testing.T) {
	tmpHome := t.TempDir()

	// Global maneater.toml with an allow rule.
	globalDir := filepath.Join(tmpHome, ".config", "maneater")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "maneater.toml"), []byte(`
[[exec.allow]]
binary = "git"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Project-local maneater.toml with a deny rule.
	projectDir := filepath.Join(tmpHome, "repos", "myproject")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "maneater.toml"), []byte(`
[[exec.deny]]
binary = "sudo"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadManeaterHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadManeaterHierarchy: %v", err)
	}

	if cfg.Exec == nil {
		t.Fatal("exec config should not be nil")
	}
	if len(cfg.Exec.Allow) != 1 || cfg.Exec.Allow[0].Binary != "git" {
		t.Errorf("expected 1 allow rule for git, got %v", cfg.Exec.Allow)
	}
	if len(cfg.Exec.Deny) != 1 || cfg.Exec.Deny[0].Binary != "sudo" {
		t.Errorf("expected 1 deny rule for sudo, got %v", cfg.Exec.Deny)
	}
}

func TestLoadManeaterHierarchyFallsBackToModelsToml(t *testing.T) {
	tmpHome := t.TempDir()

	// Only models.toml exists (no maneater.toml).
	globalDir := filepath.Join(tmpHome, ".config", "maneater")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "models.toml"), []byte(`
default = "test"

[models.test]
path = "/tmp/model.gguf"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadManeaterHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadManeaterHierarchy: %v", err)
	}

	if cfg.Default != "test" {
		t.Errorf("Default = %q, want %q", cfg.Default, "test")
	}
	if cfg.Models["test"].Path != "/tmp/model.gguf" {
		t.Errorf("Model test path = %q, want /tmp/model.gguf", cfg.Models["test"].Path)
	}
}

func TestLoadManeaterHierarchyNoConfigsIsEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	projectDir := filepath.Join(tmpHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadManeaterHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadManeaterHierarchy: %v", err)
	}

	if cfg.Exec != nil {
		t.Error("exec should be nil when no configs exist")
	}
	if len(cfg.Models) != 0 {
		t.Error("models should be empty when no configs exist")
	}
}

func TestParseManeaterExecConfig(t *testing.T) {
	input := []byte(`
[[exec.allow]]
binary = "git"
args = ["status", "diff"]

[[exec.allow]]
binary = "jq"

[[exec.deny]]
binary = "sudo"
`)
	doc, err := DecodeManeaterConfig(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cfg := doc.Data()
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
