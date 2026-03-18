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

### Merge semantics

- **Servers map**: last-writer-wins per server name. A child moxyfile defining
  `[servers.grit]` completely replaces the parent's `[servers.grit]`. New server
  names are added to the accumulated map.
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
