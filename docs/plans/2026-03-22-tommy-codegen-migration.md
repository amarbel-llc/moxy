# Tommy Codegen Migration Plan

> **For Claude:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Replace hand-rolled Parse and marshal shim with tommy's generated
codegen (approach B --- Parse keeps returning Config).

**Architecture:** Add `//go:generate tommy generate` to Config struct. Generated
`DecodeConfig`/`Encode` replace manual CST navigation in Parse and
`marshal.UnmarshalDocument`/`MarshalDocument` in WriteServer. Parse signature
stays `([]byte) (Config, error)` --- callers unchanged.

**Tech Stack:** Go, tommy codegen (`//go:generate tommy generate`), gomod2nix
for Nix builds.

**Rollback:** `git revert` the commit. Hand-rolled Parse and marshal shim are
fully deleted but trivially recoverable from git history.

**Escalation:** If approach B creates friction (e.g., callers need document
handles for comment-preserving edits), escalate to approach A where Parse
returns `*ConfigDocument`.

--------------------------------------------------------------------------------

### Task 1: Update tommy dependency

**Files:** - Modify: `go.mod` - Modify: `go.sum`

**Step 1: Update dependency**

Run: `go get github.com/amarbel-llc/tommy@latest`

**Step 2: Tidy**

Run: `go mod tidy`

**Step 3: Verify**

Run: `go vet ./...` Expected: no errors (existing code still compiles against
new tommy)

**Step 4: Commit**

    feat(config): update tommy dependency to latest

--------------------------------------------------------------------------------

### Task 2: Add MarshalTOML to Command

The codegen's `Encode()` needs `MarshalTOML` on any type that has
`UnmarshalTOML`. Command currently only has `UnmarshalTOML`.

**Files:** - Modify: `internal/config/config.go` - Test:
`internal/config/config_test.go`

**Step 1: Write failing test**

Add to `config_test.go`:

``` go
func TestCommandMarshalTOML(t *testing.T) {
    cmd := MakeCommand("grit", "mcp", "--verbose")
    got, err := cmd.MarshalTOML()
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if got != "grit mcp --verbose" {
        t.Errorf("got %q, want %q", got, "grit mcp --verbose")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v -run TestCommandMarshalTOML` Expected:
FAIL --- `MarshalTOML` not defined

**Step 3: Implement MarshalTOML**

Add to `config.go` after `UnmarshalTOML`:

``` go
func (c Command) MarshalTOML() (string, error) {
    return c.String(), nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v -run TestCommandMarshalTOML` Expected:
PASS

**Step 5: Commit**

    feat(config): add MarshalTOML to Command for codegen support

--------------------------------------------------------------------------------

### Task 3: Generate tommy companion types

**Files:** - Modify: `internal/config/config.go` (add directive) - Create:
`internal/config/config_tommy.go` (generated)

**Step 1: Add go:generate directive**

Add above `type Config struct`:

``` go
//go:generate tommy generate
type Config struct {
```

**Step 2: Run code generation**

Run: `go generate ./internal/config/`

This produces `internal/config/config_tommy.go` with `ConfigDocument`,
`DecodeConfig`, `Data()`, `Encode()`.

**Step 3: Verify it compiles**

Run: `go vet ./internal/config/...` Expected: no errors

**Step 4: Commit**

    feat(config): add tommy codegen for Config

--------------------------------------------------------------------------------

### Task 4: Replace Parse with codegen

**Files:** - Modify: `internal/config/config.go:103-181` (replace Parse, delete
helpers)

**Step 1: Replace Parse body**

Replace the entire `Parse` function, `parseCommandFromNode`, and
`parseAnnotations` with:

``` go
func Parse(data []byte) (Config, error) {
    if len(data) == 0 {
        return Config{}, nil
    }

    doc, err := DecodeConfig(data)
    if err != nil {
        return Config{}, fmt.Errorf("parsing moxyfile: %w", err)
    }

    return *doc.Data(), nil
}
```

**Step 2: Remove unused imports**

Remove `cst` and `document` imports from config.go. Keep `errors`, `fmt`,
`io/fs`, `os`, `path/filepath`, `strings`.

**Step 3: Run existing tests --- all should pass unchanged**

Run: `go test ./internal/config/... -v` Expected: all PASS (Parse signature
unchanged, same return type)

**Step 4: Run validate tests too**

Run: `go test ./internal/validate/... -v` Expected: PASS

**Step 5: Commit**

    refactor(config): replace hand-rolled Parse with tommy codegen

    Deletes parseCommandFromNode, parseAnnotations, and 30+ lines of manual
    CST navigation. Parse signature unchanged — callers unaffected.

--------------------------------------------------------------------------------

### Task 5: Replace WriteServer with codegen

**Files:** - Modify: `internal/config/tommy.go` (replace entire file)

**Step 1: Replace WriteServer and delete shim types**

Replace `tommy.go` contents with:

``` go
package config

import (
    "errors"
    "fmt"
    "io/fs"
    "os"
)

func WriteServer(path string, srv ServerConfig) error {
    data, err := os.ReadFile(path)
    if err != nil && !errors.Is(err, fs.ErrNotExist) {
        return fmt.Errorf("reading %s: %w", path, err)
    }

    doc, err := DecodeConfig(data)
    if err != nil {
        return fmt.Errorf("parsing %s: %w", path, err)
    }

    cfg := doc.Data()

    found := false
    for i, s := range cfg.Servers {
        if s.Name == srv.Name {
            cfg.Servers[i] = srv
            found = true
            break
        }
    }
    if !found {
        cfg.Servers = append(cfg.Servers, srv)
    }

    out, err := doc.Encode()
    if err != nil {
        return fmt.Errorf("encoding %s: %w", path, err)
    }

    return os.WriteFile(path, out, 0o644)
}
```

**Step 2: Run tommy tests**

Run: `go test ./internal/config/... -v -run TestWriteServer` Expected: all 3
WriteServer tests PASS

**Step 3: Commit**

    refactor(config): replace marshal shim with tommy codegen in WriteServer

    Deletes moxyfileConfig, moxyfileServer, toMoxyfileServer. WriteServer
    now uses generated DecodeConfig/Encode directly.

--------------------------------------------------------------------------------

### Task 6: Update tommy_test.go

**Files:** - Modify: `internal/config/tommy_test.go`

**Step 1: Rewrite round-trip tests to use codegen**

Replace the three `marshal.UnmarshalDocument`/`MarshalDocument` tests with
equivalent tests using `DecodeConfig`/`Encode`. Delete `TestToMoxyfileServer`
(the function no longer exists).

``` go
package config

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestCodegenNoOpRoundTrip(t *testing.T) {
    input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"\ncommand = \"grit mcp\"\n\n[[servers]]\nname = \"lux\"\ncommand = \"lux\"\n")

    doc, err := DecodeConfig(input)
    if err != nil {
        t.Fatalf("decode: %v", err)
    }

    if len(doc.Data().Servers) != 2 {
        t.Fatalf("expected 2 servers, got %d", len(doc.Data().Servers))
    }

    out, err := doc.Encode()
    if err != nil {
        t.Fatalf("encode: %v", err)
    }
    if string(out) != string(input) {
        t.Fatalf("no-op round-trip changed output.\nexpected:\n%s\ngot:\n%s", input, out)
    }
}

func TestCodegenAppendPreservesComments(t *testing.T) {
    input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"  # git operations\ncommand = \"grit mcp\"\n")

    doc, err := DecodeConfig(input)
    if err != nil {
        t.Fatalf("decode: %v", err)
    }

    cfg := doc.Data()
    cfg.Servers = append(cfg.Servers, ServerConfig{
        Name:    "chix",
        Command: MakeCommand("chix", "mcp"),
    })

    out, err := doc.Encode()
    if err != nil {
        t.Fatalf("encode: %v", err)
    }

    outStr := string(out)
    if !strings.HasPrefix(outStr, "# my MCP servers\n") {
        t.Error("top comment lost after append")
    }
    if !strings.Contains(outStr, "# git operations") {
        t.Error("inline comment lost after append")
    }
    if !strings.Contains(outStr, `name = "chix"`) {
        t.Error("appended server not found")
    }
}

func TestCodegenUpdateInPlacePreservesComments(t *testing.T) {
    input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"  # git operations\ncommand = \"grit mcp\"\n\n[[servers]]\nname = \"lux\"\ncommand = \"lux\"\n")

    doc, err := DecodeConfig(input)
    if err != nil {
        t.Fatalf("decode: %v", err)
    }

    cfg := doc.Data()
    cfg.Servers[0].Command = MakeCommand("grit", "mcp", "--verbose")

    out, err := doc.Encode()
    if err != nil {
        t.Fatalf("encode: %v", err)
    }

    outStr := string(out)
    if !strings.Contains(outStr, `command = "grit mcp --verbose"`) {
        t.Error("grit command not updated in place")
    }
    if !strings.Contains(outStr, "# git operations") {
        t.Error("inline comment lost after update")
    }
    if !strings.Contains(outStr, "# my MCP servers") {
        t.Error("top comment lost after update")
    }
}
```

Note: `TestWriteServerCreatesNewFile`, `TestWriteServerAppendsToExisting`, and
`TestWriteServerUpdatesExistingByName` remain unchanged.

**Step 2: Run all tests**

Run: `go test ./internal/config/... -v` Expected: all PASS

**Step 3: Commit**

    test(config): update tommy tests to use codegen API

--------------------------------------------------------------------------------

### Task 7: Build and integration test

**Files:** - Modify: `gomod2nix.toml` (regenerated)

**Step 1: Run full Go test suite**

Run: `just test-go` Expected: all PASS

**Step 2: Build and run bats integration tests**

Run: `just test-bats` Expected: all PASS

**Step 3: Regenerate gomod2nix and build with Nix**

Run: `just build-nix` Expected: successful build

**Step 4: Commit gomod2nix changes if any**

    chore: regenerate gomod2nix.toml
