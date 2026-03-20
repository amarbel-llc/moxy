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
just test             # all tests (go + bats)
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

**Moxyfile hierarchy** -- TOML config files loaded and merged in order:

1.  `~/.config/moxy/moxyfile` (global)
2.  Each parent directory between `$HOME` and `$CWD`
3.  `./moxyfile` (project-local)

Later files override earlier ones by server name. See `internal/config` for the
merge logic. Comment-preserving edits use `amarbel-llc/tommy` (CST-based TOML
library) in `internal/config/tommy.go`.

**Proxy flow** -- On startup, moxy loads the merged config, spawns each child
server via `internal/mcpclient` (stdio JSON-RPC), performs MCP `initialize`
handshake, then serves as a unified MCP server via `internal/proxy`.

Namespacing: tools and prompts are prefixed with `<server-name>-`, resources and
resource templates with `<server-name>/`.

**Key packages:**

- `internal/config` -- moxyfile parsing, hierarchy loading, merge semantics
- `internal/mcpclient` -- JSON-RPC client that spawns and manages child
  processes
- `internal/proxy` -- aggregates children, implements `ToolProviderV1`,
  `ResourceProviderV1`, and `PromptProviderV1`
- `internal/validate` -- TAP-14 output validation of moxyfile hierarchy
- `internal/add` -- interactive `huh` form for adding servers to a moxyfile

**CLI subcommands** (dispatched in `cmd/moxy/main.go`):

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
- The justfile sets `output-format = "tap"` for TAP output from just itself
