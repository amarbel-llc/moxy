package native

import (
	"strings"
	"testing"
)

// A moxin with several bad tool files reports all of them at once, so the
// author fixes them in one pass instead of one per restart.
func TestAllFailingToolFilesAreReportedTogether(t *testing.T) {
	good := strings.Replace(pathReaderBody, `perms-request = "always-allow"`,
		"perms-request = \"always-allow\"\npath-exempt = \"fixture\"", 1)
	dir := writeMoxinDir(t, t.TempDir(), "test", `
schema = 1
name = "test"
`, map[string]string{
		"a-bad":  pathReaderBody,
		"b-good": good,
		"c-bad":  pathReaderBody,
		"d-bad":  pathReaderBody,
	})
	_, err := ParseMoxinDir(dir)
	if err == nil {
		t.Fatal("expected a load error")
	}
	msg := err.Error()
	for _, want := range []string{"3 tool files", `"test"`, "a-bad.toml", "c-bad.toml", "d-bad.toml", "path-exempt"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
	if strings.Contains(msg, "b-good.toml") {
		t.Errorf("error %q names the file that loads", msg)
	}
}

// One failure keeps its familiar single-line shape.
func TestOneFailingToolFileKeepsItsShape(t *testing.T) {
	_, err := parsePathTool(t, `perms-request = "always-allow"`, "")
	if err == nil || !strings.HasPrefix(err.Error(), "tool file reader.toml: ") {
		t.Fatalf("got %v", err)
	}
}
