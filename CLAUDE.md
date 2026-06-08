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
just test             # the gate: nix flake check + net_cap bats + runtime smokes
just test-go          # fast devshell loop: go vet + go test (NOT the gate)
just test-bats        # fast devshell loop: bats-default lane (NOT the gate)
just test-bats-net_cap  # loopback-binding lane (streamable_http.bats)

# Single Go test
go test ./internal/config/... -v -run TestParse

# Run a focused subset of bats tests through the nix sandbox: a single
# tag's lane (tags are auto-discovered from `# bats file_tags=` directives,
# e.g. grit, folio, chix, host_only). This is the nix-only way to run
# specific bats tests — there is no devshell/raw-bats path.
just test-bats-tag grit
```

`nix flake check` is the single hermetic gate: it runs `go-test-race`
(`go test -race ./...`), `go-vet`, `go-lint` (golangci-lint), `conformist`
(fmt + dead-jq), and the `bats-default` lane — all in the build sandbox, so
env-dependent and race bugs can't slip through the way they do in the
devshell. `just test` (hence `just` / the `merge-this-session` pre-merge
hook) routes through it. The devshell `test-go` / `test-bats` / `lint-*`
recipes stay as fast inner-loop iteration; the flake check is the source of
truth. `-race` lives in the gate (#348), so it runs on every merge.

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

Moxy injects a `restart` meta tool. `restart` restarts any configured child
server by name. For persistent children, restart closes and re-spawns the
process. For ephemeral children, it re-probes capabilities.

Moxy also injects a `batch` meta tool that runs a sequence of moxin
sub-calls under a single permission prompt. Reach for it when an agent
needs to invoke a known list of similar destructive operations (e.g.
deleting 25 tags via `grit.tag`) — without `batch`, each sub-call burns a
separate approval prompt. Each sub-call's permission is resolved through
moxy's own `perms-request` machinery (in `internal/permcheck`); `deny` or
unknown (any tool without an explicit moxin perm-request, including
builtins and child MCP servers) aborts the whole batch with a single
bailout record, so one wrong tool name nukes the batch — by design.
Output is TAP-NDJSON mirroring the `amarbel-llc/tap` `pkgs/ndjson`
schema, with one `test` record per sub-call plus a final `summary`.
`on_error` controls whether mid-batch failure stops (default) or
continues — both modes are valid; stop produces `skip` directive records
for the remainder. Sequential execution only in v1. See
`docs/plans/2026-05-20-batch-tool.md`.

Moxy also injects `async`, `async-result`, and `async-cancel` meta tools
(FDR 0004): `async {tool, args, timeout?}` backgrounds one tool call (allow-only
permission preflight; tools may additionally opt out via the top-level
`permit-async = false` TOML key, #317), returns `{job_id, status:"running"}`
immediately,
and wakes the agent on the terminal state via clown's job-wakeup channel
(`${CLOWN_BIN:-clown}`); results are written to the user-level `moxy-async`
madder store (provisioned by home-manager — moxy never creates it) with the
digest embedded in the wake message. The optional `timeout` (duration string
like `"10m"`; else the 30-min default) caps wall-clock time; on expiry moxy
kills the whole process tree (`Setpgid` + group SIGTERM + `WaitDelay` at the
native exec layer, #344/#345) and terminalizes with status `timeout` — a
moxy-only status reported verbatim by `async-result` but emitted on clown's
wire as `interrupted`. For live mid-flight observability (FDR 0005 /
clown RFC-0010), moxy resolves a per-job output spool via
`clown job spool-path` and tees the native-moxin child's stdout+stderr into
it at the `runMoxinProcess` exec layer (threaded down via
`internal/spoolctx`); `async-result` on a running job then shells
`clown job status <id> --json` and surfaces `{elapsed_sec, last_activity,
spool_bytes, progress, tail}` verbatim — the clown channel is the single
source of truth, and absent it the response keeps its v1 shape. `batch
{async: true}` backgrounds a whole batch as one job. The manager lives in
`internal/asyncjob`; the single instrumentation/registration pattern mirrors
`batch`. See `docs/features/0004-async-tool-dispatch.md` and `0005-async-job-live-status.md`.

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

Result shaping (see moxin(7) RESULT SHAPING for the full system):
`result-type` picks envelope ownership (`mcp-result` default = script emits
the full MCP result, needed for dynamic mimeTypes; `text` = moxy builds it).
`cache-results` (`threshold` default | `always` | `never`) controls madder
blob writes independently of mime declaration (#319): threshold caches only
oversized outputs; `always` is for small-but-composable outputs (jq, man
sections, diffs); `content-type` is a pure mime label stamped onto cached
resource blocks — alone it never causes caching, and small uncached outputs
drop the mime.

**Moxin dependency rule:** All moxins (`moxins/*/*.toml`) must have their
external dependencies provided via nix wrapping. Never rely on tools being on
the ambient PATH --- they won't be outside the moxy devshell. The pattern: put
scripts in `libexec/`, wrap them in `flake.nix` postInstall with
`wrapProgram --set PATH`, and reference via `@LIBEXEC@` placeholder in the TOML
config. Inline shell scripts must use `command = "bash"` (not `"sh"`) since they
rely on bash features (`pipefail`, arrays, process substitution). `bash` is
provided via the nix wrapper PATH.

**Bash vs. bun/zx for moxin scripts:** bash is fine for simple pipelines, but
prefer bun + [zx](https://github.com/google/zx) for any tool that (a) parses
JSON input, (b) passes caller-controlled arguments to a subprocess, or (c) has
more than trivial output shaping. The safety argument is much shorter:
`await $\`cmd ${arrayOfArgs}\`` in zx interpolates each array element as one
distinct argv entry with no shell involvement --- compared to bash, where you
need NUL-delimited `jq` + `while read -d ''` + `"${array[@]}"` to achieve the
same property. Bun startup is negligible for tools that already spend
hundreds of ms in network/IPC calls: a hello-world ESM script measures 36 ms,
proved in the bun fork at
[`amarbel-llc/bun@d2da258bc`](https://github.com/amarbel-llc/bun/commit/d2da258bc22a1198857afd2f301c0d524a6060d2).
Bun moxins are wired through `mkBunMoxin` in `flake.nix`; scripts live in
`moxins/<name>/src/*.ts` and get compiled to `bin/` entries at nix build time
via `buildBunBinaries`. See `moxins/chix/src/flake-*.ts`, `flake-show.ts`,
`store-ls.ts` for existing examples.

### Folio (File I/O Tools)

Native server (`.moxy/servers/folio.toml`) providing file read/write operations
via inline shell commands. No separate binary.

**Tools:**

- `read` -- read a file with line numbers. Optional `start`/`end` print only
  that inclusive range; optional `delete_start`/`delete_end` omit a range
  instead (#327 folded the former read-range/read-excluding tools in).
- `glob` -- find files matching a glob pattern. Supports `**` for recursive
  matching. Results sorted by modification time (newest first).
- `write` -- create or overwrite a file (atomic write via tempfile+rename,
  creates parent directories, preserves permissions of existing files).

### Plugin Assets (Monitors, Skills, Hooks)

Moxy is shipped as a Claude Code plugin. Plugin-level features that aren't MCP
tools --- hooks, monitors, skills --- live in top-level `hooks/`, `monitors/`,
and `skills/<name>/` directories at the repo root. They're installed into
`$out/share/purse-first/moxy/<category>/` by hand-written `cp`/`substitute`
lines in the `moxy` derivation's `postInstall` in `flake.nix`. Paths that need
to resolve to `/nix/store/…` absolutes use `@TOKEN@` placeholders that
postInstall replaces --- same technique as `@MOXY@` in `hooks/pre-tool-use`.

When a plugin monitor script needs a real PATH (`tail`, `grep`, `date`, etc.),
put it in the matching moxin's `bin/` so it gets the standard `mkMoxin`
wrapping, and point the plugin manifest at the moxin's absolute binary path
via an `@…@` substitution rather than re-wrapping in the plugin dir.

### Key Packages

- `internal/config` -- moxyfile parsing, hierarchy loading, merge semantics
- `internal/mcpclient` -- JSON-RPC client that spawns and manages child
  processes
- `internal/proxy` -- aggregates children, implements `ToolProviderV1`,
  `ResourceProviderV1`, and `PromptProviderV1`
- `internal/status` -- unified status display and validation of moxyfile hierarchy
- `internal/add` -- interactive `huh` form for adding servers to a moxyfile
- `internal/paginate` -- cursor-based pagination for resource lists
- `internal/embedding` -- vector index, cosine similarity, CGo llama bindings
- `internal/statsd` -- fire-and-forget UDP statsd metrics
  (`STATSD_HOST`/`STATSD_PORT`, kill switch `MOXY_DISABLE_STATSD=1`); emit
  sites: the `Proxy.CallToolV1` wrapper (all proxied tool dispatch),
  `Proxy.ReadResource` + `Proxy.GetPromptV1` (resource_read / prompt_get
  families, #312), and serve_moxin's instrumented adapter (#311) — all
  sharing the `statsd.OutcomeFor` classifier

### CLI Subcommands

Dispatched in `cmd/moxy/main.go`:

- (default) -- run as MCP proxy server
- `status` -- show per-level config hierarchy, moxins, and validation
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
- **Bats tests for moxin scripts must invoke the nix-built binary**, not the raw
  source. Inside the nix sandbox lanes, the binary is supplied via env vars
  (`MOXY_BIN`, `MADDER_BIN`, `GRIT_BIN`, `FREUD_BIN`, …) populated by
  `mkBatsLane`'s `binaries` map. The `.bats` files fall back to
  `$BATS_TEST_DIRNAME/../result/share/moxy/moxins/<name>/bin/` only when those
  env vars are unset. Source
  scripts are committed mode `100644` and assume deps come from ambient PATH —
  only the nix wrapper has `+x` and the correct dep PATH. The wrapper appends
  (does not replace) PATH, so tests can still prepend shadow binaries like a
  `gh` stub. Tests run inside the nix build sandbox via
  `pkgs.testers.batsLane`; per-tag flake outputs (`bats-folio`, `bats-grit`,
  `bats-default`, …) are auto-discovered from `# bats file_tags=` directives
  in each `.bats` file. See `flake.nix`'s `mkBatsLane`.
- The justfile sets `output-format = "tap"` for TAP output from just itself
- Embedding tests in `internal/embedding/` require `MANPAGE_MODEL_PATH` env var
  pointing to the nomic GGUF model; they are skipped otherwise
- `search_quality_test.go` documents expected ranking behavior and known
  limitations --- update these when changing the embedding pipeline
