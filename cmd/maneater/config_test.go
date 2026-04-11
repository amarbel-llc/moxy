package main

import (
	"os"
	"path/filepath"
	"testing"
)

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

	if len(cfg.Models) != 0 {
		t.Error("models should be empty when no configs exist")
	}
}

func TestLoadManeaterHierarchyExpandsEnvInModelPath(t *testing.T) {
	tmpHome := t.TempDir()

	globalDir := filepath.Join(tmpHome, ".config", "maneater")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "maneater.toml"), []byte(`
default = "test"

[models.test]
path = "$MANEATER_TEST_MODEL_DIR/model.gguf"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MANEATER_TEST_MODEL_DIR", "/tmp/models")

	cfg, err := LoadManeaterHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadManeaterHierarchy: %v", err)
	}

	want := "/tmp/models/model.gguf"
	if cfg.Models["test"].Path != want {
		t.Errorf("Model test path = %q, want %q", cfg.Models["test"].Path, want)
	}
}

func TestLoadManeaterHierarchyExpandsEnvBraceSyntax(t *testing.T) {
	tmpHome := t.TempDir()

	globalDir := filepath.Join(tmpHome, ".config", "maneater")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "maneater.toml"), []byte(`
default = "test"

[models.test]
path = "${MANEATER_TEST_DATA}/maneater/models/nomic.gguf"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(tmpHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MANEATER_TEST_DATA", "/home/user/.local/share")

	cfg, err := LoadManeaterHierarchy(tmpHome, projectDir)
	if err != nil {
		t.Fatalf("LoadManeaterHierarchy: %v", err)
	}

	want := "/home/user/.local/share/maneater/models/nomic.gguf"
	if cfg.Models["test"].Path != want {
		t.Errorf("Model test path = %q, want %q", cfg.Models["test"].Path, want)
	}
}

func TestMergeConfigManpathIncludeAccumulates(t *testing.T) {
	base := ManeaterConfig{
		Manpath: &ManpathConfig{
			Include: []string{"/base/man"},
		},
	}
	overlay := ManeaterConfig{
		Manpath: &ManpathConfig{
			Include: []string{"/overlay/man"},
		},
	}

	merged := MergeConfig(base, overlay)
	if merged.Manpath == nil {
		t.Fatal("merged manpath should not be nil")
	}
	if len(merged.Manpath.Include) != 2 {
		t.Fatalf("expected 2 include paths, got %d", len(merged.Manpath.Include))
	}
	if merged.Manpath.Include[0] != "/base/man" {
		t.Errorf("first include = %q, want /base/man", merged.Manpath.Include[0])
	}
	if merged.Manpath.Include[1] != "/overlay/man" {
		t.Errorf("second include = %q, want /overlay/man", merged.Manpath.Include[1])
	}
}

func TestMergeConfigManpathNoAutoOverlays(t *testing.T) {
	base := ManeaterConfig{
		Manpath: &ManpathConfig{NoAuto: false},
	}
	overlay := ManeaterConfig{
		Manpath: &ManpathConfig{NoAuto: true},
	}

	merged := MergeConfig(base, overlay)
	if !merged.Manpath.NoAuto {
		t.Error("overlay no-auto=true should override base no-auto=false")
	}
}

func TestMergeConfigManpathBaseOnlyPreserved(t *testing.T) {
	base := ManeaterConfig{
		Manpath: &ManpathConfig{
			Include: []string{"/base/man"},
			NoAuto:  true,
		},
	}
	overlay := ManeaterConfig{}

	merged := MergeConfig(base, overlay)
	if merged.Manpath == nil || len(merged.Manpath.Include) != 1 {
		t.Error("base manpath should be preserved when overlay has none")
	}
	if !merged.Manpath.NoAuto {
		t.Error("base no-auto should be preserved")
	}
}

func TestParseManpathConfig(t *testing.T) {
	input := []byte(`
[manpath]
include = ["/extra/man", "vendor/man"]
no-auto = true
`)
	doc, err := DecodeManeaterConfig(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	cfg := doc.Data()
	if cfg.Manpath == nil {
		t.Fatal("manpath config should not be nil")
	}
	if len(cfg.Manpath.Include) != 2 {
		t.Fatalf("expected 2 include paths, got %d", len(cfg.Manpath.Include))
	}
	if !cfg.Manpath.NoAuto {
		t.Error("no-auto should be true")
	}
}
