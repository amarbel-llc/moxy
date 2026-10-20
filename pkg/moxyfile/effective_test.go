package moxyfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const effectiveFixture = `
include = ["ignored-by-parse"]
progressive-disclosure = true
builtin-native = false
disable-servers = ["zed", "alpha"]
disable-moxins = ["man.semantic-search", "chix", "chix"]

[credentials]
read = "vault read moxy/{name}"
write = "vault write moxy/{name}"
delete = "vault delete moxy/{name}"

[[servers]]
name = "web"
url = "https://example.invalid/mcp"
headers-helper = "token-helper --for web"

[servers.headers]
X-Second = "two"
Authorization = "Bearer $MOXY_TEST_SECRET"

[servers.oauth]
client-id = "abc"
callback-port = 8123

[[servers]]
name = "lux"
command = ["lux", "--dir", "/path with spaces", "say \"hi\""]
nix-devshell = "."
paginate = true
ephemeral = false

[servers.annotations]
readOnlyHint = true
`

func TestRenderEffectiveRoundTripsAndIsCanonical(t *testing.T) {
	t.Setenv("MOXY_TEST_SECRET", "hunter2")

	cfg, err := ParseLiteral([]byte(effectiveFixture))
	if err != nil {
		t.Fatal(err)
	}
	first := RenderEffective(cfg)

	reparsed, err := ParseLiteral(first)
	if err != nil {
		t.Fatalf("render does not parse: %v\n%s", err, first)
	}
	if second := RenderEffective(reparsed); string(second) != string(first) {
		t.Errorf("render is not a fixed point:\n--- first\n%s\n--- second\n%s", first, second)
	}

	out := string(first)
	for _, want := range []string{
		`command = ["lux", "--dir", "/path with spaces", "say \"hi\""]`,
		`disable-moxins = ["chix", "man.semantic-search"]`,
		`disable-servers = ["alpha", "zed"]`,
		`"Authorization" = "Bearer $MOXY_TEST_SECRET"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"hunter2", "include"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("render contains %q:\n%s", unwanted, out)
		}
	}
	if strings.Index(out, `name = "lux"`) > strings.Index(out, `name = "web"`) {
		t.Errorf("servers are not sorted by name:\n%s", out)
	}

	lux := reparsed.Servers[0]
	if got := lux.Command.Parts(); len(got) != 4 || got[2] != "/path with spaces" {
		t.Errorf("command argv did not survive the round trip: %q", got)
	}
}

func TestRenderEffectiveOfEmptyConfigIsEmpty(t *testing.T) {
	if got := RenderEffective(Config{}); len(got) != 0 {
		t.Errorf("got %q", got)
	}
}

func TestLiteralLoadKeepsHeaderReferencesAndPlainLoadExpandsThem(t *testing.T) {
	t.Setenv("MOXY_TEST_SECRET", "hunter2")
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	moxyfile := "[[servers]]\nname = \"web\"\nurl = \"https://example.invalid/mcp\"\n[servers.headers]\nAuthorization = \"Bearer $MOXY_TEST_SECRET\"\n"
	if err := os.WriteFile(filepath.Join(dir, "moxyfile"), []byte(moxyfile), 0o644); err != nil {
		t.Fatal(err)
	}

	literal, err := LoadHierarchyLiteral(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := literal.Merged.Servers[0].Headers["Authorization"]; got != "Bearer $MOXY_TEST_SECRET" {
		t.Errorf("literal load expanded the header: %q", got)
	}

	expanded, err := LoadHierarchy(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := expanded.Merged.Servers[0].Headers["Authorization"]; got != "Bearer hunter2" {
		t.Errorf("plain load did not expand the header: %q", got)
	}
}

func TestLoadReferenceLiteral(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "repo")
	if err := os.MkdirAll(filepath.Join(home, ".config", "moxy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".config", "moxy", "moxyfile"), "[[servers]]\nname = \"global\"\ncommand = \"g\"\n")
	write(filepath.Join(dir, "extra.toml"), "[[servers]]\nname = \"included\"\ncommand = \"i\"\n")
	write(filepath.Join(dir, "moxyfile"), "include = [\"extra.toml\"]\n[[servers]]\nname = \"local\"\ncommand = \"l\"\n")

	names := func(h Hierarchy) string {
		var out []string
		for _, s := range h.Merged.Servers {
			out = append(out, s.Name)
		}
		return strings.Join(out, ",")
	}

	asDir, err := LoadReferenceLiteral(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(asDir); got != "global,included,local" {
		t.Errorf("directory reference walks the hierarchy: got %q", got)
	}

	asFile, err := LoadReferenceLiteral(home, filepath.Join(dir, "moxyfile"))
	if err != nil {
		t.Fatal(err)
	}
	if got := names(asFile); got != "included,local" {
		t.Errorf("file reference is the file plus its includes only: got %q", got)
	}

	if _, err := LoadReferenceLiteral(home, filepath.Join(dir, "absent")); err == nil {
		t.Error("a missing reference must be an error, not an empty config")
	}
}
