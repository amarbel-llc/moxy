# Config-as-Server: Declarative MCP Tool Framework

**Date:** 2026-04-08
**Status:** Proposed

## Problem

Moxy currently requires every MCP capability to be a separate Go binary
(maneater, folio). Each binary implements the full MCP protocol, JSON-RPC
transport, and tool/resource registration. This is heavyweight for tools that are
fundamentally "run a command, return the output" — the MCP protocol machinery
dwarfs the domain logic.

## Solution

Extend moxy from a pure proxy into a declarative MCP server framework. Tool
capabilities can be expressed as TOML config files in `.moxy/` directories. Moxy
natively handles MCP protocol, namespacing, process invocation, result caching,
and resource-as-fd composition. The config declares what's executable — no
separate permission system needed.

## Architecture

### ServerBackend Interface

The proxy currently couples to `*mcpclient.Client`. We introduce an interface:

```go
type ServerBackend interface {
    Call(ctx context.Context, method string, params any) (json.RawMessage, error)
    Notify(method string, params any) error
    SetOnNotification(fn func(*jsonrpc.Message))
    Name() string
    Close() error
}
```

Two implementations:

- **`mcpclient.Client`** — existing proxied child servers (already satisfies
  this interface, no changes needed)
- **`native.Server`** — interprets `.moxy/*.toml` configs, dispatches tool calls
  by spawning declared binaries

The proxy's `Child` struct changes from `*mcpclient.Client` to
`ServerBackend`. The `ConnectFunc` signature changes accordingly. At startup,
native servers synthesize `InitializeResultV1` from config (declared tools →
capabilities).

### Config Format

Each `.moxy/*.toml` file declares one virtual server:

```toml
name = "shell"
description = "Shell execution with result caching"

[[tools]]
name = "exec"
description = "Execute a shell command"
command = "sh"
args = ["-c"]

[tools.input.properties.command]
type = "string"
description = "Shell command to execute"

[tools.input]
required = ["command"]
```

A tool declares:

- `name`, `description` — MCP tool metadata
- `command`, `args` — the binary moxy spawns per invocation
- `input` — JSON Schema for the tool's parameters (OpenAPI-spec-like)

The exact mapping from tool input parameters to process invocation (args, stdin,
env) will be determined through prototyping.

### Config Discovery

`.moxy/` directories follow the same hierarchy walk as moxyfiles:

1. `~/.config/moxy/.moxy/` (global)
2. Each parent directory between `$HOME` and `$CWD`
3. `$CWD/.moxy/` (project-local)

Each `*.toml` file in a `.moxy/` directory defines one virtual server. Files at
lower levels override higher ones by server name (same merge semantics as
moxyfiles).

### Result Caching

All config-as-server tool outputs are automatically cached with sensible
defaults (no configuration knobs in MVP). Cached results are addressable via a
URI scheme. When output exceeds a token threshold, a head+tail summary is
returned with a URI pointing to the full content.

### Resource-as-fd Composition

Cached result URIs appearing in tool input strings are rewritten to `/dev/fd/N`
file descriptors. The cached content is streamed into the child process via OS
pipes. This is the same mechanism currently in maneater's `exec_substitute.go`,
lifted to the proxy level so all config-as-servers get it.

This is the composability primitive: output from one tool becomes input to
another without round-tripping through the LLM context window.

### Dual Architecture

The maneater binary continues working as a regular child server alongside
config-as-servers. Freud and folio have been fully migrated to native
config-as-servers (`.moxy/servers/freud.toml` + `.moxy/bin/freud-*` Python
scripts, `.moxy/servers/folio.toml` with inline shell commands).

## MVP Scope

**In scope:**

- `ServerBackend` interface extraction
- `.moxy/*.toml` config parsing and discovery (hierarchy walk)
- `native.Server` implementation (tool dispatch via process spawning)
- Result caching with sensible defaults (no config)
- Resource-as-fd rewriting (cached URIs → `/dev/fd/N`)
- Prototype target: express maneater's `exec` tool as `.moxy/shell.toml`

**Deferred:**

- Resource declarations (only tools in MVP)
- Embedding generation for resources
- Per-tool or per-server caching configuration
- Allow/deny permission rules (the config is the permission)
- Migrating maneater to config-as-server (freud + folio migrations complete)

## Prototype Target

Express maneater's `exec` tool as a `.moxy/shell.toml` config. Verify
end-to-end: tool call → process spawn → result caching → cached result fed back
via `/dev/fd/N` into a subsequent call.

## Rollback Strategy

- **Dual architecture** — removing `.moxy/` files reverts to existing setup
- **No changes to existing binaries** — maneater is untouched
- **Additive interface extraction** — `mcpclient.Client` already satisfies
  `ServerBackend`, so the refactor is safe
- **Promotion criteria:** config-based exec produces identical results to
  maneater exec for the same commands over 1 week of daily use
- **Rollback procedure:** delete `.moxy/` configs; existing child servers resume
  handling all tool calls
