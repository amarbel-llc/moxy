# `moxy add` Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Add an interactive `moxy add [path]` command that walks the user through creating a new server entry and appends it to a moxyfile.

**Architecture:** New `internal/add` package with a `huh` form (name, command, annotations multi-select). Generates a `[[servers]]` TOML block and appends it to the target file (default `./moxyfile`, creates if missing). Wired into `cmd/moxy/main.go` as a subcommand.

**Tech Stack:** `github.com/charmbracelet/huh` for the interactive form, `github.com/BurntSushi/toml` (already in go.mod) for serialization context.

**Rollback:** N/A — purely additive.

---

### Task 1: Add huh dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Step 1: Add the dependency**

Run: `go get github.com/charmbracelet/huh`

**Step 2: Tidy**

Run: `go mod tidy`

**Step 3: Commit**

```
git add go.mod go.sum
git commit -m "deps: add charmbracelet/huh for interactive forms"
```

---

### Task 2: Add TOML serialization support to config.Command

The `Command` type has a custom `UnmarshalTOML` but no way to produce TOML
output. We need a `String()` method to render the command for appending to a
moxyfile.

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing test**

In `config_test.go`, add:

```go
func TestCommandString(t *testing.T) {
	tests := []struct {
		name   string
		parts  []string
		expect string
	}{
		{"single word", []string{"grit"}, "grit"},
		{"multiple words", []string{"grit", "mcp", "--verbose"}, "grit mcp --verbose"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := makeCommand(tt.parts...)
			if got := cmd.String(); got != tt.expect {
				t.Errorf("got %q, want %q", got, tt.expect)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestCommandString -v`
Expected: FAIL — `Command` has no `String()` method.

**Step 3: Write minimal implementation**

In `config.go`, add to the `Command` type:

```go
func (c Command) String() string {
	return strings.Join(c.parts, " ")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestCommandString -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add Command.String() for TOML serialization"
```

---

### Task 3: Create internal/add package with TOML generation

The `add` package handles form display and file writing. This task covers the
TOML generation and file append logic (testable without huh).

**Files:**
- Create: `internal/add/add.go`
- Create: `internal/add/add_test.go`

**Step 1: Write the failing tests**

```go
package add

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amarbel-llc/moxy/internal/config"
)

func TestFormatServerBlock(t *testing.T) {
	srv := config.ServerConfig{
		Name:    "grit",
		Command: config.MakeCommand("grit", "mcp"),
	}
	got := FormatServerBlock(srv)
	if !strings.Contains(got, `[[servers]]`) {
		t.Error("expected [[servers]] header")
	}
	if !strings.Contains(got, `name = "grit"`) {
		t.Error("expected name field")
	}
	if !strings.Contains(got, `command = "grit mcp"`) {
		t.Error("expected command field")
	}
}

func TestFormatServerBlockWithAnnotations(t *testing.T) {
	ro := true
	srv := config.ServerConfig{
		Name:    "grit",
		Command: config.MakeCommand("grit"),
		Annotations: &config.AnnotationFilter{
			ReadOnlyHint: &ro,
		},
	}
	got := FormatServerBlock(srv)
	if !strings.Contains(got, `readOnlyHint = true`) {
		t.Error("expected readOnlyHint annotation")
	}
}

func TestFormatServerBlockNoAnnotations(t *testing.T) {
	srv := config.ServerConfig{
		Name:    "lux",
		Command: config.MakeCommand("lux"),
	}
	got := FormatServerBlock(srv)
	if strings.Contains(got, "annotations") {
		t.Error("should not include annotations when none set")
	}
}

func TestAppendToFileCreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moxyfile")

	srv := config.ServerConfig{
		Name:    "grit",
		Command: config.MakeCommand("grit"),
	}
	if err := AppendServerToFile(path, srv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `name = "grit"`) {
		t.Errorf("expected grit in file, got:\n%s", data)
	}
}

func TestAppendToFileAppendsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moxyfile")

	existing := "[[servers]]\nname = \"grit\"\ncommand = \"grit\"\n"
	os.WriteFile(path, []byte(existing), 0o644)

	srv := config.ServerConfig{
		Name:    "lux",
		Command: config.MakeCommand("lux"),
	}
	if err := AppendServerToFile(path, srv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, `name = "grit"`) {
		t.Error("existing content should be preserved")
	}
	if !strings.Contains(content, `name = "lux"`) {
		t.Error("new server should be appended")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/add/ -v`
Expected: FAIL — package doesn't exist yet.

**Step 3: Write implementation**

`internal/add/add.go`:

```go
package add

import (
	"fmt"
	"os"
	"strings"

	"github.com/amarbel-llc/moxy/internal/config"
)

// FormatServerBlock produces a [[servers]] TOML block for appending to a
// moxyfile.
func FormatServerBlock(srv config.ServerConfig) string {
	var b strings.Builder
	b.WriteString("[[servers]]\n")
	fmt.Fprintf(&b, "name = %q\n", srv.Name)
	fmt.Fprintf(&b, "command = %q\n", srv.Command.String())

	if srv.Annotations != nil {
		var parts []string
		if srv.Annotations.ReadOnlyHint != nil && *srv.Annotations.ReadOnlyHint {
			parts = append(parts, "readOnlyHint = true")
		}
		if srv.Annotations.DestructiveHint != nil && *srv.Annotations.DestructiveHint {
			parts = append(parts, "destructiveHint = true")
		}
		if srv.Annotations.IdempotentHint != nil && *srv.Annotations.IdempotentHint {
			parts = append(parts, "idempotentHint = true")
		}
		if srv.Annotations.OpenWorldHint != nil && *srv.Annotations.OpenWorldHint {
			parts = append(parts, "openWorldHint = true")
		}
		if len(parts) > 0 {
			fmt.Fprintf(&b, "annotations = { %s }\n", strings.Join(parts, ", "))
		}
	}

	return b.String()
}

// AppendServerToFile appends a [[servers]] block to the given moxyfile path.
// Creates the file if it doesn't exist.
func AppendServerToFile(path string, srv config.ServerConfig) error {
	block := FormatServerBlock(srv)

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var content string
	if len(existing) > 0 {
		s := string(existing)
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		content = s + "\n" + block
	} else {
		content = block
	}

	return os.WriteFile(path, []byte(content), 0o644)
}
```

This also requires exporting `MakeCommand` from config for tests. Add to
`config.go`:

```go
func MakeCommand(parts ...string) Command {
	return Command{parts: parts}
}
```

**Step 4: Run tests**

Run: `go test ./internal/add/ -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/add/add.go internal/add/add_test.go internal/config/config.go
git commit -m "feat(add): TOML generation and file append for server entries"
```

---

### Task 4: Add interactive huh form

**Files:**
- Modify: `internal/add/add.go`

**Step 1: Add the Run function with huh form**

```go
import (
	"os/exec"
	"github.com/charmbracelet/huh"
)

// Run shows the interactive form and appends the result to the moxyfile at
// path.
func Run(path string) error {
	var name, command string
	var annotations []string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Server name").
				Description("Unique name for this MCP server").
				Value(&name).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("name is required")
					}
					return nil
				}),

			huh.NewInput().
				Title("Command").
				Description("Executable to run (must be on $PATH)").
				Value(&command).
				Validate(func(s string) error {
					fields := strings.Fields(s)
					if len(fields) == 0 {
						return fmt.Errorf("command is required")
					}
					if _, err := exec.LookPath(fields[0]); err != nil {
						return fmt.Errorf("%q not found on $PATH", fields[0])
					}
					return nil
				}),

			huh.NewMultiSelect[string]().
				Title("Annotations").
				Description("Select annotation hints for this server's tools").
				Options(
					huh.NewOption("readOnlyHint", "readOnlyHint"),
					huh.NewOption("destructiveHint", "destructiveHint"),
					huh.NewOption("idempotentHint", "idempotentHint"),
					huh.NewOption("openWorldHint", "openWorldHint"),
				).
				Value(&annotations),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	srv := buildServerConfig(name, command, annotations)
	return AppendServerToFile(path, srv)
}

func buildServerConfig(name, command string, annotations []string) config.ServerConfig {
	srv := config.ServerConfig{
		Name:    name,
		Command: config.MakeCommand(strings.Fields(command)...),
	}

	if len(annotations) > 0 {
		af := &config.AnnotationFilter{}
		for _, a := range annotations {
			t := true
			switch a {
			case "readOnlyHint":
				af.ReadOnlyHint = &t
			case "destructiveHint":
				af.DestructiveHint = &t
			case "idempotentHint":
				af.IdempotentHint = &t
			case "openWorldHint":
				af.OpenWorldHint = &t
			}
		}
		srv.Annotations = af
	}

	return srv
}
```

**Step 2: Verify compile**

Run: `go build ./internal/add/`
Expected: success

**Step 3: Commit**

```
git add internal/add/add.go
git commit -m "feat(add): interactive huh form for server entry"
```

---

### Task 5: Wire up in main.go

**Files:**
- Modify: `cmd/moxy/main.go`

**Step 1: Add the add subcommand**

After the existing `validate` block, add:

```go
if flag.NArg() >= 1 && flag.Arg(0) == "add" {
	path := "moxyfile"
	if flag.NArg() >= 2 {
		path = flag.Arg(1)
	}
	if err := add.Run(path); err != nil {
		log.Fatalf("add: %v", err)
	}
	return
}
```

Add `"github.com/amarbel-llc/moxy/internal/add"` to imports.

**Step 2: Verify compile**

Run: `go build ./cmd/moxy/`
Expected: success

**Step 3: Manual test**

Run: `go run ./cmd/moxy add`
Expected: interactive form appears, fills in a server, appends to `./moxyfile`.

**Step 4: Commit**

```
git add cmd/moxy/main.go
git commit -m "feat: wire up moxy add subcommand"
```

---

### Task 6: Run full test suite

**Step 1: Run all Go tests**

Run: `go test ./... -v`
Expected: all pass

**Step 2: Run bats tests**

Run: `just test-bats` (or via devShell)
Expected: all pass
