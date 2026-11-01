package native

import (
	"strings"
	"testing"
)

func parseResourceTool(t *testing.T, tool string) (*ParseResult, error) {
	t.Helper()
	dir := writeMoxinDir(t, t.TempDir(), "test", `
schema = 1
name = "test"
`, map[string]string{"tool": tool})
	return ParseMoxinDirFull(dir)
}

func TestParseMoxinDirResources(t *testing.T) {
	t.Run("omitted declares nothing", func(t *testing.T) {
		result, err := parseResourceTool(t, `
schema = 3
command = "echo"
[input.properties.path]
type = "string"
`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := result.Config.Tools[0].Resources; len(got) != 0 {
			t.Errorf("Resources = %v, want none", got)
		}
	})

	t.Run("declares inputs as resources with an operation", func(t *testing.T) {
		result, err := parseResourceTool(t, `
schema = 3
command = "echo"
[input.properties.source]
type = "string"
[input.properties.destination]
type = "string"
[resources.source]
op = "read"
[resources.destination]
op = "write"
`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := result.Config.Tools[0].Resources
		want := []ResourceArg{
			{Param: "destination", Op: "write"},
			{Param: "source", Op: "read"},
		}
		if len(got) != len(want) {
			t.Fatalf("Resources = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("Resources[%d] = %v, want %v", i, got[i], want[i])
			}
		}
		if len(result.Undecoded) > 0 {
			t.Errorf("unexpected undecoded keys: %v", result.Undecoded)
		}
	})

	t.Run("split marks a whitespace-separated list", func(t *testing.T) {
		result, err := parseResourceTool(t, `
schema = 3
command = "echo"
[input.properties.paths]
type = "string"
[resources.paths]
op = "write"
split = true
`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := result.Config.Tools[0].Resources; len(got) != 1 || !got[0].Split {
			t.Errorf("Resources = %v, want one split resource", got)
		}
	})

	t.Run("an unknown input is an error", func(t *testing.T) {
		_, err := parseResourceTool(t, `
schema = 3
command = "echo"
[input.properties.path]
type = "string"
[resources.nope]
op = "read"
`)
		if err == nil || !strings.Contains(err.Error(), `resources names "nope"`) {
			t.Fatalf("err = %v, want unknown-input error", err)
		}
	})

	t.Run("an unknown operation is an error", func(t *testing.T) {
		_, err := parseResourceTool(t, `
schema = 3
command = "echo"
[input.properties.path]
type = "string"
[resources.path]
op = "delete"
`)
		if err == nil || !strings.Contains(err.Error(), `invalid op "delete"`) {
			t.Fatalf("err = %v, want invalid-op error", err)
		}
	})

	t.Run("a missing operation is an error", func(t *testing.T) {
		_, err := parseResourceTool(t, `
schema = 3
command = "echo"
[input.properties.path]
type = "string"
[resources.path]
`)
		if err == nil || !strings.Contains(err.Error(), "op") {
			t.Fatalf("err = %v, want missing-op error", err)
		}
	})

	t.Run("a non-string input is an error", func(t *testing.T) {
		_, err := parseResourceTool(t, `
schema = 3
command = "echo"
[input.properties.count]
type = "integer"
[resources.count]
op = "read"
`)
		if err == nil || !strings.Contains(err.Error(), "string") {
			t.Fatalf("err = %v, want string-input error", err)
		}
	})
}
