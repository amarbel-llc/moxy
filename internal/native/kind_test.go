package native

import (
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

const (
	goodTool = `
schema = 3
command = "echo"
`
	refusedTool = `
schema = 3
command = "echo"

[annotations]
kind = "list"
`
)

func discoveredNames(result DiscoverResult) []string {
	names := make([]string, len(result.Configs))
	for i, cfg := range result.Configs {
		names[i] = cfg.Name
	}
	return names
}

// A refused moxin must not be replaced by a same-named one further down the
// path: that would serve other tools and permission tiers under its name.
func TestDiscoverRefusedMoxinSuppressesLowerPrioritySameName(t *testing.T) {
	userDir, systemDir := t.TempDir(), t.TempDir()
	// The user's override lives in a directory not named after the moxin, so
	// the claim has to come from _moxin.toml.
	override := writeMoxinDir(t, userDir, "my-override", `
schema = 1
name = "grit"
`, map[string]string{"status": refusedTool})
	bundled := writeMoxinDir(t, systemDir, "grit", `
schema = 1
name = "grit"
`, map[string]string{"status": goodTool})
	writeMoxinDir(t, systemDir, "other", `
schema = 1
name = "other"
`, map[string]string{"x": goodTool})

	for name, discoverFn := range map[string]func(string, string) (DiscoverResult, error){
		"DiscoverAll":     DiscoverAll,
		"DiscoverServing": DiscoverServing,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := discoverFn(userDir, systemDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := discoveredNames(result); len(got) != 1 || got[0] != "other" {
				t.Errorf("served moxins = %v, want [other]", got)
			}
			if len(result.Refused) != 1 || result.Refused[0].Name != "grit" || result.Refused[0].Dir != override {
				t.Errorf("Refused = %+v, want grit at %s", result.Refused, override)
			}
			if len(result.Shadowed) != 1 || result.Shadowed[0].Dir != bundled || result.Shadowed[0].By != override {
				t.Errorf("Shadowed = %+v, want %s shadowed by %s", result.Shadowed, bundled, override)
			}
		})
	}

	configs, err := DiscoverConfigs(userDir, systemDir)
	if err != nil {
		t.Fatalf("DiscoverConfigs: %v", err)
	}
	for _, cfg := range configs {
		if cfg.Name == "grit" {
			t.Errorf("DiscoverConfigs served grit from %s", cfg.SourceDir)
		}
	}
	sources, err := ResolvedMoxinSources(userDir, t.TempDir(), systemDir)
	if err != nil {
		t.Fatalf("ResolvedMoxinSources: %v", err)
	}
	if dir, ok := sources["grit"]; ok {
		t.Errorf("ResolvedMoxinSources resolves grit to %s", dir)
	}
}

// The other direction is unchanged: a broken moxin low in the path does not
// take down a good one above it.
func TestDiscoverGoodMoxinWinsOverLowerPriorityRefusedOne(t *testing.T) {
	userDir, systemDir := t.TempDir(), t.TempDir()
	good := writeMoxinDir(t, userDir, "grit", `
schema = 1
name = "grit"
`, map[string]string{"status": goodTool})
	writeMoxinDir(t, systemDir, "grit", `
schema = 1
name = "grit"
`, map[string]string{"status": refusedTool})

	result, err := DiscoverServing(userDir, systemDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Configs) != 1 || result.Configs[0].SourceDir != good {
		t.Errorf("Configs = %+v, want grit from %s", result.Configs, good)
	}
	if len(result.Refused) != 0 {
		t.Errorf("Refused = %+v, want none", result.Refused)
	}
	if len(result.Errors) != 1 {
		t.Errorf("Errors = %+v, want the one load failure", result.Errors)
	}
}

// A directory whose _moxin.toml cannot name it claims its own directory name.
func TestDiscoverUnnamedRefusedMoxinClaimsItsDirectoryName(t *testing.T) {
	userDir, systemDir := t.TempDir(), t.TempDir()
	writeMoxinDir(t, userDir, "grit", `this is not toml`, map[string]string{"status": goodTool})
	writeMoxinDir(t, systemDir, "grit", `
schema = 1
name = "grit"
`, map[string]string{"status": goodTool})

	result, err := DiscoverServing(userDir, systemDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Configs) != 0 {
		t.Errorf("Configs = %v, want none", discoveredNames(result))
	}
	if len(result.Refused) != 1 || result.Refused[0].Name != "grit" {
		t.Errorf("Refused = %+v, want grit", result.Refused)
	}
}
