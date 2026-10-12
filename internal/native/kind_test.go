package native

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const kindTestMeta = `
schema = 1
name = "test"
`

func TestParseMoxinDirKind(t *testing.T) {
	dir := writeMoxinDir(t, t.TempDir(), "test", kindTestMeta, map[string]string{
		"declared": `
schema = 3
command = "echo"
kind = "list"
`,
		"undeclared": `
schema = 3
command = "echo"
`,
	})

	result, err := ParseMoxinDirFull(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Undecoded) > 0 {
		t.Errorf("kind reported as undecoded: %v", result.Undecoded)
	}
	kinds := map[string]string{}
	for _, tool := range result.Config.Tools {
		kinds[tool.Name] = tool.Kind
	}
	if kinds["declared"] != "list" {
		t.Errorf("declared kind = %q, want %q", kinds["declared"], "list")
	}
	if kinds["undeclared"] != "" {
		t.Errorf("undeclared kind = %q, want empty", kinds["undeclared"])
	}
}

// A key the [annotations] table has no field for used to vanish silently.
func TestParseMoxinDirRefusesUnknownAnnotationKeys(t *testing.T) {
	cases := map[string]string{
		"kind in the table": `
schema = 3
command = "echo"

[annotations]
read-only-hint = true
kind = "list"
`,
		"camelCase hint": `
schema = 3
command = "echo"

[annotations]
readOnlyHint = true
`,
		"inline table": `
schema = 3
command = "echo"
annotations = { read-only-hint = true, kind = "list" }
`,
		"dotted key": `
schema = 3
command = "echo"
annotations.kind = "list"
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := writeMoxinDir(t, t.TempDir(), "test", kindTestMeta, map[string]string{"api": body})
			_, err := ParseMoxinDir(dir)
			if err == nil {
				t.Fatal("expected a load error, got none")
			}
			if !strings.Contains(err.Error(), "api.toml") || !strings.Contains(err.Error(), "[annotations]") {
				t.Errorf("error does not name the file and table: %v", err)
			}
		})
	}
}

func TestParseMoxinDirAcceptsEveryKnownAnnotationKey(t *testing.T) {
	dir := writeMoxinDir(t, t.TempDir(), "test", kindTestMeta, map[string]string{
		"api": `
schema = 3
command = "echo"

[annotations]
title = "T"
read-only-hint = true
destructive-hint = false
idempotent-hint = true
open-world-hint = false
`,
	})
	if _, err := ParseMoxinDir(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The refusal is new, so every moxin this repo ships must already pass it.
func TestShippedMoxinsParse(t *testing.T) {
	root := filepath.Join("..", "..", "moxins")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}
	parsed := 0
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "_moxin.toml")); err != nil {
			continue
		}
		if _, err := ParseMoxinDir(dir); err != nil {
			t.Errorf("%s: %v", e.Name(), err)
		}
		parsed++
	}
	if parsed == 0 {
		t.Fatalf("no moxins found under %s", root)
	}
}
