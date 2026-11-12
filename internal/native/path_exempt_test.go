package native

import (
	"strings"
	"testing"
)

// moxy#520: an always-allow tool with a path-looking input and no
// [resources] stanza is a load error unless it says why with path-exempt.

const pathReaderBody = `
schema = 3
command = "echo"
perms-request = "always-allow"

[input]
type = "object"

[input.properties.path]
type = "string"
`

func parsePathTool(t *testing.T, extra, tail string) (*NativeConfig, error) {
	t.Helper()
	dir := writeMoxinDir(t, t.TempDir(), "test", `
schema = 1
name = "test"
`, map[string]string{
		"reader": strings.Replace(pathReaderBody, `perms-request = "always-allow"`, extra, 1) + tail,
	})
	return ParseMoxinDir(dir)
}

func TestUndeclaredPathReaderIsALoadError(t *testing.T) {
	_, err := parsePathTool(t, `perms-request = "always-allow"`, "")
	if err == nil {
		t.Fatal("expected a load error for an undeclared path reader")
	}
	for _, want := range []string{"reader.toml", "path", "path-exempt", "[resources"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestPathExemptWithAReasonLoads(t *testing.T) {
	cfg, err := parsePathTool(t, "perms-request = \"always-allow\"\npath-exempt = \"the guard is the gate\"", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Tools[0].PathExempt; got != "the guard is the gate" {
		t.Errorf("PathExempt = %q", got)
	}
}

func TestPathExemptWithoutAReasonIsRefused(t *testing.T) {
	_, err := parsePathTool(t, "perms-request = \"always-allow\"\npath-exempt = \"  \"", "")
	if err == nil || !strings.Contains(err.Error(), "path-exempt") {
		t.Fatalf("expected an empty path-exempt to be refused, got %v", err)
	}
}

func TestAResourcesStanzaSatisfiesThePathRule(t *testing.T) {
	_, err := parsePathTool(t, `perms-request = "always-allow"`, "\n[resources.path]\nop = \"read\"\n")
	if err != nil {
		t.Fatalf("a declared path input should load: %v", err)
	}
}

func TestOnlyAlwaysAllowToolsAreHeldToThePathRule(t *testing.T) {
	for _, tier := range []string{"each-use", "delegate-to-client", ""} {
		extra := ""
		if tier != "" {
			extra = `perms-request = "` + tier + `"`
		}
		if _, err := parsePathTool(t, extra, ""); err != nil {
			t.Errorf("tier %q: %v", tier, err)
		}
	}
}

func TestPathExemptOnAToolTheRuleDoesNotCoverIsRefused(t *testing.T) {
	// A stale exemption is noise that hides the next real gap.
	_, err := parsePathTool(t, "perms-request = \"each-use\"\npath-exempt = \"stale\"", "")
	if err == nil || !strings.Contains(err.Error(), "path-exempt") {
		t.Fatalf("expected a pointless path-exempt to be refused, got %v", err)
	}
}

func TestPathLikeInputs(t *testing.T) {
	in := &InputSchema{Properties: map[string]PropertySchema{
		"repo_path": {}, "name": {}, "count": {}, "flake_dir": {}, "pattern": {},
	}}
	got := strings.Join(PathLikeInputs(in), ",")
	if got != "flake_dir,name,repo_path" {
		t.Errorf("PathLikeInputs = %q", got)
	}
	if PathLikeInputs(nil) != nil {
		t.Error("nil schema should have no path inputs")
	}
}
