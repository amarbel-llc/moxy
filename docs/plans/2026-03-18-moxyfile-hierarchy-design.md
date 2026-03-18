# Moxyfile Hierarchy Merge

## Problem

Moxy currently loads a single `moxyfile` from the current directory. Users who
work across multiple repos with shared MCP servers must duplicate server
definitions in every repo's moxyfile. A hierarchy of moxyfiles — global,
intermediate, and repo-local — would allow shared defaults with per-repo
overrides.

## Design

### Hierarchy (load order, lowest to highest priority)

1. `~/.config/moxy/moxyfile` — global defaults
2. Each intermediate parent directory between `$HOME` and cwd, walking down
   (e.g. `~/eng/moxyfile`, `~/eng/repos/moxyfile`)
3. `<cwd>/moxyfile` — repo-local

### Moxyfile format

Servers are a TOML array-of-tables with explicit `name` and `command` fields.
Command accepts a string (split on whitespace) or an array (preserves args with
spaces). Per-server annotation filters use inline table syntax for clarity.

```toml
[[servers]]
name = "grit"
command = "grit mcp"

[[servers]]
name = "lux"
command = ["lux", "--lsp-dir", "/path with spaces"]
annotations = { readOnlyHint = true }
```

### Merge semantics

- **Servers**: last-writer-wins per server name. A child moxyfile defining a
  server with the same `name` completely replaces the parent's definition. New
  server names are appended, preserving insertion order.
- No clear sentinel — to remove an inherited server, override it in a lower
  layer (future work if needed).

### Error handling

- Missing moxyfiles at any layer are silently skipped (return empty `Config`).
- After merging all layers, error if the merged result has zero servers.

### Return type

```go
type LoadSource struct {
    Path  string
    Found bool
    File  Config
}

type Hierarchy struct {
    Sources []LoadSource
    Merged  Config
}
```

`LoadHierarchy(home, dir string) (Hierarchy, error)` walks the hierarchy and
returns both per-source detail and the final merged config.

`LoadDefaultHierarchy() (Hierarchy, error)` resolves `home` and `cwd`
automatically.

### Files changed

- `internal/config/config.go` — add `Parse`, `Merge`, `LoadHierarchy`,
  `LoadDefaultHierarchy`, `Hierarchy`, `LoadSource`; existing `Load` becomes a
  thin wrapper over `Parse`
- `internal/config/config_test.go` — tests for parse, merge, and hierarchy
- `cmd/moxy/main.go` — switch from `config.Load("moxyfile")` to
  `config.LoadDefaultHierarchy()`, validate merged server count

## Decisions

- Mirrors the spinclass sweatfile hierarchy pattern for consistency across the
  toolchain.
- Chose last-writer-wins over deep merge for server configs because server
  definitions are small and fully restating one in a repo moxyfile is not
  burdensome.
- Kept the "must have at least one server" invariant but moved validation to
  after the merge.
