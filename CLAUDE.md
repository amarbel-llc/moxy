# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with
code in this repository.

## Overview

Moxy is an MCP (Model Context Protocol) proxy that aggregates multiple child MCP
servers into a single unified server. It spawns child servers as subprocesses,
communicates via JSON-RPC over stdio, and namespaces their tools, resources, and
prompts (e.g., `grit-status`, `chix-build`). Bridges MCP protocol V0 and V1
automatically.

## Build & Test

``` sh
just                  # build + test (default target)
just build-go         # go build only -> build/moxy
just build-nix        # nix build (runs gomod2nix first)
just test             # all tests (go + bats + validate-mcp)
just test-go          # go vet + go test
just test-bats        # bats integration tests (builds first)

# Single Go test
go test ./internal/config/... -v -run TestParse

# Single bats test file
just test-bats-file moxyfile_hierarchy.bats
```

After changing Go dependencies: `just build-gomod2nix` to regenerate
`gomod2nix.toml` before `build-nix`.

## Architecture

### Moxyfile Hierarchy

TOML config files loaded and merged in order:

1.  `~/.config/moxy/moxyfile` (global)
2.  Each parent directory between `$HOME` and `$CWD`
3.  `./moxyfile` (project-local)

Later files override earlier ones by server name. See `internal/config` for the
merge logic. Comment-preserving edits use `amarbel-llc/tommy` (CST-based TOML
library) in `internal/config/tommy.go`. The `config_tommy.go` file is generated
by `//go:generate tommy generate` --- do not edit it directly.

### Proxy Flow

On startup, moxy loads the merged config, spawns each non-ephemeral child server
via `internal/mcpclient` (stdio JSON-RPC), performs MCP `initialize` handshake,
then serves as a unified MCP server via `internal/proxy`.

### Snob-Case Naming Convention

Tool and prompt names from child servers are namespaced as
`<server-name>-<snob_case_name>`. "Snob case" converts hyphens to underscores in
the child's tool/prompt name, so hyphens only appear as the server name
separator. This allows `splitLastPrefix` to unambiguously route
`server-name-tool_name` back to the correct child. Resources and resource
templates use `<server-name>/` prefix with a slash separator instead.

### Ephemeral Server Mode

Servers can be configured as ephemeral (`ephemeral = true` per-server or
globally). Ephemeral servers are not kept running --- moxy probes them at
startup to discover their capabilities/tools/resources/prompts, then shuts them
down. They are re-spawned on demand for each tool call and shut down again
after. The `restart` meta tool re-probes ephemeral servers to refresh cached
capabilities.

### Meta Tools

Moxy injects a `restart` tool (namespaced as `moxy-restart`) that restarts any
configured child server by name. For persistent children, it closes and
re-spawns the process. For ephemeral children, it re-probes capabilities.

### Synthetic Resource Tools

When `generate-resource-tools = true` on a server config, moxy generates
`resource-read` and `resource-templates` tools for that child, allowing clients
that only support tools (not resources) to access resources.

### Annotation Filtering

Server configs can include `[servers.annotations]` filters (`readOnlyHint`,
`destructiveHint`, `idempotentHint`, `openWorldHint`) to expose only tools
matching specific annotation values from a child server.

### Pagination

The `internal/paginate` package provides cursor-based pagination for resource
lists. Servers with `paginate = true` in their config get paginated resource
responses using `?offset=N&limit=M` query parameters on resource URIs.

### Key Packages

- `internal/config` -- moxyfile parsing, hierarchy loading, merge semantics
- `internal/mcpclient` -- JSON-RPC client that spawns and manages child
  processes
- `internal/proxy` -- aggregates children, implements `ToolProviderV1`,
  `ResourceProviderV1`, and `PromptProviderV1`
- `internal/validate` -- TAP-14 output validation of moxyfile hierarchy
- `internal/add` -- interactive `huh` form for adding servers to a moxyfile
- `internal/paginate` -- cursor-based pagination for resource lists

### CLI Subcommands

Dispatched in `cmd/moxy/main.go`:

- (default) -- run as MCP proxy server
- `validate` -- validate moxyfile hierarchy, output TAP-14
- `add [path]` -- interactive form to add a server entry
- `install-mcp` / `generate-plugin` / `hook` -- purse-first integration

## Dependencies

Built with `go-mcp` from `amarbel-llc/purse-first`. The `command.App`, `server`,
`transport`, and `protocol` packages provide the MCP framework. Uses `gomod2nix`
for Nix builds.

## Testing Conventions

- Go unit tests live alongside source files (`_test.go`)
- Bats integration tests in `zz-tests_bats/` use a temp `$HOME` and test the
  actual binary. Helper functions are in `common.bash`
- Bats tests use `bats-assert`, `bats-island`, and `bats-emo` helper libraries
- `run_moxy_mcp` in `common.bash` sends a full JSON-RPC initialize handshake
  then a method call, returning the result as JSON in `$output`
- `run_moxy_mcp_two` sends two method calls in one session (for testing restart,
  etc.)
- The justfile sets `output-format = "tap"` for TAP output from just itself
