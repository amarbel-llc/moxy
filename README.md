# moxy

MCP proxy that aggregates multiple child MCP servers into a single unified
server.

## Overview

Moxy spawns child MCP servers as subprocesses, communicates with them via
JSON-RPC over stdio, and presents their tools, resources, and prompts through a
single unified MCP server. Child server capabilities are namespaced with a dot
separator (e.g. `grit.status`, `chix.build`).

Configuration is loaded from a hierarchy of TOML moxyfiles — global
(`~/.config/moxy/moxyfile`), per-directory, and project-local — with later files
overriding earlier ones by server name. Moxy also discovers declarative tool
configs called **moxins** from `MOXIN_PATH`, so you can add MCP tools without
writing any Go code.

## Why moxy

Traditional MCP servers are standalone programs that each handle their own
protocol negotiation, process lifecycle, and output management. Moxy replaces
that per-server boilerplate with a shared runtime that provides several features
out of the box:

**Result caching and progressive disclosure.** Tool outputs exceeding 50 tokens
are cached to disk and replaced with a `moxy.native://results/{session}/{id}`
URI plus a head/tail summary. Agents see enough to decide whether the full output
matters, and can read the cached URI for the complete result — without blowing up
the context window on a 10,000-line `git log`. Truncation warnings are explicit
so agents never mistake partial output for complete output.

**Composable result URIs.** The `moxy.native://` URIs are first-class across
moxin tools. A `folio.read` call can take the URI from a previous `rg.search`
result as its file path; a `jq.jq` call can take one as stdin. Moxy rewrites
these to file descriptors at invocation time, so tools chain without the agent
needing to copy data between calls.

**Declarative tool authoring.** Moxins are TOML files — a manifest plus one file
per tool. Each tool file declares its name, description, input schema, command,
and args. Moxy handles MCP protocol, argument passing, process invocation,
result caching, and permission signaling. No SDK, no boilerplate, no server
code.

**Permission control.** Each moxin tool can declare a `perms-request` field:
`always-allow` (skip confirmation), `each-use` (always prompt), or
`delegate-to-client` (let the client decide). This lets read-only tools like
`folio.read` run without interrupting the agent, while destructive tools like
`grit.push` require explicit approval.

**Unified discovery.** Agents see all tools from all servers through a single MCP
connection. Built-in `moxy://` resources let agents introspect available servers,
tool counts, and full JSON schemas at runtime without additional configuration.

## Install

### Homebrew (macOS)

Installs the `moxy` binary and all shipped moxins.

```sh
brew tap oven-sh/bun       # required for bun dependency
brew tap amarbel-llc/moxy
brew install moxy
```

### Ad-hoc (single moxin)

Installs just the `moxy` binary and one moxin of your choice to
`~/.local/bin` and `~/.local/share/moxy/moxins`. Automatically registers the
moxin with Claude Code if `claude` is on PATH.

```sh
# Interactive menu
curl -fsSL https://github.com/amarbel-llc/moxy/releases/latest/download/install-moxin.bash | bash

# Direct install (e.g. grit)
curl -fsSL https://github.com/amarbel-llc/moxy/releases/latest/download/install-moxin.bash | bash -s -- grit
```

### Nix

```sh
nix run github:amarbel-llc/moxy
```

Or add to your flake inputs for the full package with nix-wrapped moxins.

## Moxins

A moxin is a directory of TOML files that defines an MCP server without any
code. The directory contains a `_moxin.toml` manifest and one `.toml` file per
tool:

```
moxins/grit/
  _moxin.toml        # server name, description
  log.toml           # tool: show commit history
  diff.toml          # tool: show changes
  commit.toml        # tool: create a commit
  ...
```

Each tool file declares its input schema, command, and args. Moxy handles
everything else — MCP protocol, argument passing (schema-ordered, then
alphabetical), process invocation, result caching, and permission signaling.
See [moxin(7)](cmd/moxy/moxin.7) for the full authoring guide.

### Shipped moxins

The following moxins ship with moxy. Each can be served individually via
`moxy serve-moxin --name <name>` or aggregated through the proxy.

| Moxin | Tools | Description | Deps |
|-------|------:|-------------|------|
| calendar | 1 | Google Calendar: view upcoming events and agendas | bun, gws |
| car | 5 | Google Drive: search, list, get, and export files | bun, gws |
| conch | 1 | Shell inspection: syntax checking and script analysis | bash |
| env | 5 | Environment inspection: PATH binaries and env vars | — |
| folio | 16 | File I/O scoped to current working directory | jq, coreutils |
| folio-external | 13 | File I/O for paths outside the current working directory | jq, coreutils |
| freud | 12 | Past Claude Code session transcripts | python3 |
| get-hubbed | 31 | GitHub tools for the current repository | gh, jq, bun |
| get-hubbed-external | 10 | GitHub tools for other repositories | gh, jq, bun |
| gmail | 2 | Gmail: triage and read messages | bun, gws |
| grit | 31 | Git operations (force-push/hard-reset blocked on main/master) | git, jq |
| gws | 1 | Google Workspace: generic API passthrough | bun, gws |
| hamster | 8 | Go package documentation via `go doc` | go, bun |
| jq | 1 | Execute jq filters on JSON data | jq |
| just-us-agents | 6 | Justfile recipe runner | just, jq, bun |
| man | 4 | Unix man page reader with section-level progressive disclosure | pandoc, mandoc |
| piers | 13 | Google Docs: read, create, edit, and comment on documents | bun, gws |
| prison | 1 | Google Sheets: read spreadsheet data | bun, gws |
| rg | 1 | Ripgrep code search with structured output modes | ripgrep |
| sisyphus | 10 | Jira Cloud tools | python3, atlassian-python-api |
| slip | 0 | Google Slides: read and edit presentations | bun, gws |

## Usage

### As a Claude Code plugin

```sh
moxy install-claude-plugin
```

This registers moxy as a Claude Code MCP server plugin. All moxins discovered
from `MOXIN_PATH` are served through the proxy alongside any servers defined in
your moxyfile hierarchy.

### Serve a single moxin

```sh
moxy serve-moxin --name grit
```

Serves one moxin as a standalone MCP server over stdio. Useful for registering
individual moxins with MCP clients directly.

### Run as MCP proxy

```sh
moxy serve-mcp
```

Loads the moxyfile hierarchy, spawns child servers, discovers moxins, and serves
everything through a single MCP endpoint on stdio. This is the default command
when invoked via the MCP protocol.

## Configuration

Moxy loads TOML moxyfiles from a directory hierarchy:

1. `~/.config/moxy/moxyfile` (global)
2. Each parent directory between `$HOME` and the current directory
3. `./moxyfile` (project-local)

Later files override earlier ones by server name. See
[moxyfile(5)](cmd/moxy/moxyfile.5) for the full configuration reference.

## Documentation

Moxy ships with man pages:

- **moxy(1)** — command overview and subcommands
- **moxyfile(5)** — configuration file format and hierarchy
- **moxy-hooks(5)** — hook configuration for Claude Code integration
- **moxin(7)** — moxin format and authoring guide

View them with `man moxy`, `man moxyfile`, `man moxy-hooks`, or `man moxin`
after installing via Homebrew or nix.

## License

[MIT](LICENSE)
