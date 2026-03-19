# Use Tommy for Comment-Preserving Moxyfile Edits

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Replace string-append in `moxy add` with tommy's marshal API so comments survive round-trip edits and duplicate server names are updated in place.

**Architecture:** Add tommy-compatible adapter structs (`moxyfileConfig`/`moxyfileServer`) in `internal/config` that use plain `string` for command and value-type for annotations (tommy's marshal doesn't support custom unmarshal interfaces or pointer-to-struct fields). The `add` package switches from `FormatServerBlock` + `AppendServerToFile` to a single `WriteServer` function that does parse→modify→write via tommy. Config reading (`Parse`/`Load`/`LoadHierarchy`) and validation stay on BurntSushi/toml for now — swapping those is a separate task.

**Tech Stack:** `github.com/amarbel-llc/tommy/pkg/marshal`

**Rollback:** Revert the add package changes; the old `FormatServerBlock`/`AppendServerToFile` functions remain until task 2 removes them.

---

### Task 1: Add tommy adapter types and conversion functions

**Promotion criteria:** N/A — new code.

**Files:**
- Create: `internal/config/tommy.go`
- Test: `internal/config/tommy_test.go` (already exists with eval tests — extend it)

**Step 1: Write the failing test**

In `internal/config/tommy_test.go`, replace the existing eval tests with conversion tests:

```go
package config

import (
	"strings"
	"testing"

	"github.com/amarbel-llc/tommy/pkg/marshal"
)

func TestTommyNoOpRoundTrip(t *testing.T) {
	input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"  # git operations\ncommand = \"grit mcp\"\n\n[[servers]]\nname = \"lux\"\ncommand = \"lux\"\n")

	var mf moxyfileConfig
	handle, err := marshal.UnmarshalDocument(input, &mf)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(mf.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(mf.Servers))
	}

	out, err := marshal.MarshalDocument(handle, &mf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != string(input) {
		t.Fatalf("no-op round-trip changed output.\nexpected:\n%s\ngot:\n%s", input, out)
	}
}

func TestTommyAppendPreservesComments(t *testing.T) {
	input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"  # git operations\ncommand = \"grit mcp\"\n")

	var mf moxyfileConfig
	handle, err := marshal.UnmarshalDocument(input, &mf)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	mf.Servers = append(mf.Servers, moxyfileServer{Name: "chix", Command: "chix mcp"})
	out, err := marshal.MarshalDocument(handle, &mf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
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

func TestTommyUpdateInPlacePreservesComments(t *testing.T) {
	input := []byte("# my MCP servers\n\n[[servers]]\nname = \"grit\"  # git operations\ncommand = \"grit mcp\"\n\n[[servers]]\nname = \"lux\"\ncommand = \"lux\"\n")

	var mf moxyfileConfig
	handle, err := marshal.UnmarshalDocument(input, &mf)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	mf.Servers[0].Command = "grit mcp --verbose"
	out, err := marshal.MarshalDocument(handle, &mf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
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

func TestToMoxyfileServer(t *testing.T) {
	ro := true
	srv := ServerConfig{
		Name:    "grit",
		Command: MakeCommand("grit", "mcp"),
		Annotations: &AnnotationFilter{
			ReadOnlyHint: &ro,
		},
	}
	mf := toMoxyfileServer(srv)
	if mf.Name != "grit" {
		t.Errorf("name: got %q", mf.Name)
	}
	if mf.Command != "grit mcp" {
		t.Errorf("command: got %q", mf.Command)
	}
	if !mf.ReadOnlyHint {
		t.Error("expected readOnlyHint true")
	}
}

func TestToMoxyfileServerNoAnnotations(t *testing.T) {
	srv := ServerConfig{
		Name:    "grit",
		Command: MakeCommand("grit"),
	}
	mf := toMoxyfileServer(srv)
	if mf.ReadOnlyHint || mf.DestructiveHint || mf.IdempotentHint || mf.OpenWorldHint {
		t.Error("expected all annotation hints false")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run "TestTommy|TestToMoxyfile" ./internal/config/`
Expected: FAIL — `moxyfileConfig`, `moxyfileServer`, `toMoxyfileServer` undefined.

**Step 3: Write minimal implementation**

Create `internal/config/tommy.go`:

```go
package config

// moxyfileConfig is the tommy-compatible representation of a moxyfile.
// Uses plain types that tommy's reflection-based marshal supports.
type moxyfileConfig struct {
	Servers []moxyfileServer `toml:"servers"`
}

// moxyfileServer is the tommy-compatible representation of a server entry.
// Command is a plain string (tommy can't handle custom UnmarshalTOML).
// Annotation bools are flattened (tommy can't handle pointer-to-struct).
type moxyfileServer struct {
	Name            string `toml:"name"`
	Command         string `toml:"command"`
	ReadOnlyHint    bool   `toml:"readOnlyHint"`
	DestructiveHint bool   `toml:"destructiveHint"`
	IdempotentHint  bool   `toml:"idempotentHint"`
	OpenWorldHint   bool   `toml:"openWorldHint"`
}

func toMoxyfileServer(srv ServerConfig) moxyfileServer {
	mf := moxyfileServer{
		Name:    srv.Name,
		Command: srv.Command.String(),
	}
	if srv.Annotations != nil {
		if srv.Annotations.ReadOnlyHint != nil {
			mf.ReadOnlyHint = *srv.Annotations.ReadOnlyHint
		}
		if srv.Annotations.DestructiveHint != nil {
			mf.DestructiveHint = *srv.Annotations.DestructiveHint
		}
		if srv.Annotations.IdempotentHint != nil {
			mf.IdempotentHint = *srv.Annotations.IdempotentHint
		}
		if srv.Annotations.OpenWorldHint != nil {
			mf.OpenWorldHint = *srv.Annotations.OpenWorldHint
		}
	}
	return mf
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run "TestTommy|TestToMoxyfile" ./internal/config/`
Expected: PASS

**Step 5: Commit**

```
feat(config): add tommy adapter types for comment-preserving moxyfile edits
```

---

### Task 2: Rewrite add package to use tommy parse-modify-write

**Promotion criteria:** Once this works, `FormatServerBlock` and `AppendServerToFile` can be removed.

**Files:**
- Modify: `internal/config/tommy.go` — add `WriteServer` function
- Modify: `internal/config/tommy_test.go` — add `WriteServer` tests
- Modify: `internal/add/add.go` — call `config.WriteServer` instead of `AppendServerToFile`
- Modify: `internal/add/add_test.go` — update tests for new behavior

**Step 1: Write the failing tests for WriteServer**

Append to `internal/config/tommy_test.go`:

```go
func TestWriteServerCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moxyfile")

	srv := ServerConfig{
		Name:    "grit",
		Command: MakeCommand("grit", "mcp"),
	}
	if err := WriteServer(path, srv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, `name = "grit"`) {
		t.Errorf("expected grit in file, got:\n%s", content)
	}
	if !strings.Contains(content, `command = "grit mcp"`) {
		t.Errorf("expected command in file, got:\n%s", content)
	}
}

func TestWriteServerAppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moxyfile")
	os.WriteFile(path, []byte("# config\n\n[[servers]]\nname = \"grit\"\ncommand = \"grit\"\n"), 0o644)

	srv := ServerConfig{
		Name:    "lux",
		Command: MakeCommand("lux"),
	}
	if err := WriteServer(path, srv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "# config") {
		t.Error("comment lost")
	}
	if !strings.Contains(content, `name = "grit"`) {
		t.Error("existing server lost")
	}
	if !strings.Contains(content, `name = "lux"`) {
		t.Error("new server not appended")
	}
}

func TestWriteServerUpdatesExistingByName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "moxyfile")
	os.WriteFile(path, []byte("# config\n\n[[servers]]\nname = \"grit\"  # git\ncommand = \"grit\"\n"), 0o644)

	srv := ServerConfig{
		Name:    "grit",
		Command: MakeCommand("grit", "mcp", "--verbose"),
	}
	if err := WriteServer(path, srv); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "# config") {
		t.Error("top comment lost")
	}
	if !strings.Contains(content, "# git") {
		t.Error("inline comment lost")
	}
	if !strings.Contains(content, `command = "grit mcp --verbose"`) {
		t.Error("command not updated")
	}
	if strings.Count(content, `name = "grit"`) != 1 {
		t.Error("duplicate grit entry")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v -run "TestWriteServer" ./internal/config/`
Expected: FAIL — `WriteServer` undefined.

**Step 3: Write minimal implementation**

Append to `internal/config/tommy.go`:

```go
import (
	"errors"
	"io/fs"
	"os"

	"github.com/amarbel-llc/tommy/pkg/marshal"
)

func WriteServer(path string, srv ServerConfig) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var mf moxyfileConfig
	handle, err := marshal.UnmarshalDocument(data, &mf)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	newEntry := toMoxyfileServer(srv)

	found := false
	for i, s := range mf.Servers {
		if s.Name == srv.Name {
			mf.Servers[i] = newEntry
			found = true
			break
		}
	}
	if !found {
		mf.Servers = append(mf.Servers, newEntry)
	}

	out, err := marshal.MarshalDocument(handle, &mf)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}

	return os.WriteFile(path, out, 0o644)
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v -run "TestWriteServer" ./internal/config/`
Expected: PASS

**Step 5: Update add package to use WriteServer**

In `internal/add/add.go`, replace the call and remove dead code:

- Remove `FormatServerBlock` and `AppendServerToFile`
- Change `Run` to call `config.WriteServer(path, srv)` instead of `AppendServerToFile(path, srv)`

**Step 6: Update add tests**

In `internal/add/add_test.go`:

- Remove `TestFormatServerBlock`, `TestFormatServerBlockWithAnnotations`, `TestFormatServerBlockNoAnnotations`
- Remove `TestAppendToFileCreatesNew`, `TestAppendToFileAppendsExisting`
- Keep `TestBuildServerConfigNoAnnotations`, `TestBuildServerConfigWithAnnotations`

**Step 7: Run all tests**

Run: `go test ./...`
Expected: all PASS

**Step 8: Commit**

```
feat(add): use tommy for comment-preserving moxyfile writes

WriteServer does parse→modify→write via tommy's marshal API.
Updates existing servers by name instead of appending duplicates.
Comments and formatting are preserved byte-for-byte.

Removes FormatServerBlock and AppendServerToFile.
```

---

### Task 3: Update ADR status

**Files:**
- Modify: `docs/decisions/0001-use-tommy-for-comment-preserving-moxyfile-edits.md`

**Step 1: Update status from `proposed` to `experimental`**

Change frontmatter `status: proposed` → `status: experimental`.

**Step 2: Commit**

```
docs: promote tommy ADR to experimental
```
