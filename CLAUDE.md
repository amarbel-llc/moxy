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
just build-go         # go build only -> build/{moxy,maneater}
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

### Dot-Separator Naming Convention

Tool and prompt names from child servers are namespaced as
`<server-name>.<original-tool-name>`. The dot separator is unambiguous because
server names must not contain dots (validated at config load). `splitPrefix` on
the first dot recovers the server name and original tool/prompt name exactly ---
no encoding or decoding is needed. Resources and resource templates use
`<server-name>/` prefix with a slash separator instead. Server names may contain
hyphens (e.g., `just-us-agents.list-recipes`).

### Ephemeral Server Mode

Servers can be configured as ephemeral (`ephemeral = true` per-server or
globally). Ephemeral servers are not kept running --- moxy probes them at
startup to discover their capabilities/tools/resources/prompts, then shuts them
down. They are re-spawned on demand for each tool call and shut down again
after. The `restart` meta tool re-probes ephemeral servers to refresh cached
capabilities.

### Meta Tools

Moxy injects `restart` and `exec-mcp` tools. `restart` restarts any configured
child server by name. `exec-mcp` invokes a tool on a named child server
directly. For persistent children, restart closes and re-spawns the process. For
ephemeral children, it re-probes capabilities.

### Synthetic Resource Tools

When `generate-resource-tools = true` on a server config, moxy generates
`resource-read` and `resource-templates` tools for that child, allowing clients
that only support tools (not resources) to access resources.

### Discovery Resources

Moxy provides built-in `moxy://` resources for agent discovery:

- `moxy://servers` --- JSON array of all child servers with capability counts
  and status (running/failed)
- `moxy://servers/{server}` --- single server details
- `moxy://tools/{server}` --- tool names and descriptions for a child server
- `moxy://tools/{server}/{tool}` --- full JSON schema for a specific tool

The `Instructions` field in the initialize response is dynamic, built from child
server capability counts at startup. It lists each server with tool/resource
counts so agents know what's available before making any calls.

Unknown resource reads return structured JSON hints pointing agents to
`moxy://servers` or `moxy://tools/{server}` instead of raw error messages.

### Annotation Filtering

Server configs can include `[servers.annotations]` filters (`readOnlyHint`,
`destructiveHint`, `idempotentHint`, `openWorldHint`) to expose only tools
matching specific annotation values from a child server.

### Pagination

The `internal/paginate` package provides cursor-based pagination for resource
lists. Servers with `paginate = true` in their config get paginated resource
responses using `?offset=N&limit=M` query parameters on resource URIs.

### Moxins (Config-as-Server)

Moxins are declarative MCP tool configs discovered via `MOXIN_PATH`. Each moxin
is a directory containing a `_moxin.toml` manifest and one `.toml` file per
tool. Moxy's `internal/native` package handles MCP protocol, namespacing, result
caching, and resource-as-fd composition. Moxins require no Go code --- tool
schemas are declared in TOML, dispatch is by process invocation.

Directory layout:

```
moxins/<name>/
  _moxin.toml          # schema, name, description
  <tool-name>.toml     # schema, command, args, input schema
```

The `_moxin.toml` manifest declares server identity (`schema`, `name`,
`description`). Each tool file uses a flat structure (`[input]` not
`[tools.input]`). Tools may declare `perms-request` to control permission
behavior: `always-allow`, `each-use`, or `delegate-to-client` (default).

**Moxin dependency rule:** All moxins (`moxins/*/*.toml`) must have their
external dependencies provided via nix wrapping. Never rely on tools being on
the ambient PATH --- they won't be outside the moxy devshell. The pattern: put
scripts in `libexec/`, wrap them in `flake.nix` postInstall with
`wrapProgram --set PATH`, and reference via `@LIBEXEC@` placeholder in the TOML
config. Inline shell scripts must use `command = "bash"` (not `"sh"`) since they
rely on bash features (`pipefail`, arrays, process substitution). `bash` is
provided via the nix wrapper PATH.

### Folio (File I/O Tools)

Native server (`.moxy/servers/folio.toml`) providing file read/write operations
via inline shell commands. No separate binary.

**Tools:**

- `read` -- read an entire file with line numbers. Large files (>2000 lines)
  return a head+tail summary; use `read_range` for specific sections.
- `read_range` -- read an inclusive line range from a file.
- `read_excluding` -- read a file with an inclusive line range omitted.
- `glob` -- find files matching a glob pattern. Supports `**` for recursive
  matching. Results sorted by modification time (newest first).
- `write` -- create or overwrite a file (atomic write via tempfile+rename,
  creates parent directories, preserves permissions of existing files).

### Key Packages

- `internal/config` -- moxyfile parsing, hierarchy loading, merge semantics
- `internal/mcpclient` -- JSON-RPC client that spawns and manages child
  processes
- `internal/proxy` -- aggregates children, implements `ToolProviderV1`,
  `ResourceProviderV1`, and `PromptProviderV1`
- `internal/validate` -- TAP-14 output validation of moxyfile hierarchy
- `internal/add` -- interactive `huh` form for adding servers to a moxyfile
- `internal/paginate` -- cursor-based pagination for resource lists
- `internal/embedding` -- vector index, cosine similarity, CGo llama bindings

### CLI Subcommands

Dispatched in `cmd/moxy/main.go`:

- (default) -- run as MCP proxy server
- `validate` -- validate moxyfile hierarchy, output TAP-14
- `add [path]` -- interactive form to add a server entry
- `install-mcp` / `generate-plugin` / `hook` -- purse-first integration

## Dependencies

Built with `go-mcp` from `amarbel-llc/purse-first`. The `command.App`, `server`,
`transport`, and `protocol` packages provide the MCP framework. Uses `gomod2nix`
for Nix builds. Maneater additionally links against `llama-cpp` via CGo for
embedding generation.

## Testing Conventions

- Go unit tests live alongside source files (`_test.go`)
- Bats integration tests in `zz-tests_bats/` use a temp `$HOME` and test the
  actual binary. Helper functions are in `common.bash`
- Bats tests use `bats-assert`, `bats-island`, and `bats-emo` helper libraries
- `run_moxy_mcp` in `common.bash` sends a full JSON-RPC initialize handshake
  then a method call to moxy, returning the result as JSON in `$output`
- `run_moxy_mcp_two` sends two method calls in one session (for testing restart,
  etc.)
- The justfile sets `output-format = "tap"` for TAP output from just itself
- Embedding tests in `internal/embedding/` require `MANPAGE_MODEL_PATH` env var
  pointing to the nomic GGUF model; they are skipped otherwise
- `search_quality_test.go` documents expected ranking behavior and known
  limitations --- update these when changing the embedding pipeline
