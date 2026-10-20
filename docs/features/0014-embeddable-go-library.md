---
status: proposed
date: 2026-10-20
issue: moxy#487
related: moxy#485, moxy#486, FDR 0004 (async), FDR 0011 (dispatch-time consent),
  FDR 0013 (narrowing); clown FDR 0021 (headless agent loop), clown#249
  (godyn go.nix), clown#258 (juggler extraction)
promotion-criteria: >
  proposed → experimental when `pkg/moxy` exists with Open / Tools / Call /
  Close, a Go test under `pkg/moxy` drives a fixture moxin through the public
  surface with no CLI involved, `moxy serve mcp` and `moxy serve-http` are
  thin wrappers over it with every existing Go and bats test green, and
  `nix build .#go-pkgs` produces a consumable producer tree. experimental →
  testing when clown (or the extracted juggler) links `code.linenisgreat.com/moxy/pkg/moxy`
  through its go.nix bridge and a headless worker calls a moxin tool in
  process under an enforce policy (moxy#486).
---

# Moxy as an embeddable Go library (`pkg/moxy`)

## Problem Statement

moxy is a process. Everything that makes it useful — loading and judging a
moxyfile, discovering moxins, spawning and supervising child MCP servers,
namespacing tools, resolving a tool's permission tier, substituting
`madder://blobs/<digest>` references in and out of a call — lives under
`internal/`, reachable only through `moxy serve mcp` (stdio) or `moxy
serve-http` (streamable HTTP), with permission *enforcement* delegated to a
third process, the Claude Code hook (`moxy hook`). clown's headless agent loop
(clown FDR 0021) runs juggler as the agent with no hook layer in front; it
wants tool listing, a call with a caller identity and a permission policy, and
blob-reference handling in process, with one implementation shared by the
CLI entry points so the library and the binary cannot drift. The operator's
direction: "we're going to need to refactor moxy to support its use as a
native go library for clown and juggler to use", and "refactor moxy first",
then the serve-time enforce mode (moxy#486) on the new surface.

clown FDR 0021 §3.3 recommended the opposite order (enforce mode over MCP
first, embed later). The operator ruled the library first; #486 is designed
to land *on* the surface this record proposes, so the policy object exists
once, for both the embedder and the serving binary.

## Inventory: the call path today

Read in this worktree at bb446c3. Line numbers are approximate anchors.

| Stage | Where | What it does | Library concern |
| --- | --- | --- | --- |
| load + judge config | `cmd/moxy/narrowing.go` `loadServingConfig`, `refuseUnlessNarrows`, `loadJudgedConfigAtStartup` | reads the hierarchy literal (`config.LoadDefaultHierarchyLiteral`), pins `MOXY_PARENT_MOXYFILE` once per **process** (`pinnedParentReference`, a `sync.OnceValue`), pins startup `allow-tools` in a **package global** (`startupAllowTools`), judges via `narrowing.CheckReload`, then expands header `$VAR`s | process globals must become per-instance state; the env var becomes an option with the env as the CLI's default |
| bootstrap | `cmd/moxy/bootstrap.go` `bootstrap` | moxin discovery (`native.DiscoverServing`), `[[servers]]` spawn through a `ConnectFunc`, moxin → `native.Server` children, refused moxins recorded as failed servers | this is the "construct a proxy from a moxyfile" the issue asks for; it is in `package main` today |
| engine | `internal/proxy/proxy.go` `New`, setters `SetToolAllow/SetToolFilter/SetNameTemplate/SetMadderClient/SetSessionID/SetMoxinReloader/SetBootstrapper/SetResolver/SetBuiltinTools/SetAsyncManager/SetNotifier` | holds children, failed list, ephemeral metadata, registries, the permission resolver (used only by batch/async) | the facade constructs this; the setter soup becomes one options struct |
| meta tools | `cmd/moxy/main.go` `runServer` ~618–738 | registers `restart`, `batch`, `async`, `async-result`, `async-cancel` plus go-mcp's `status` on a `server.ToolRegistryV1` | belongs in the library (same tools for an embedder that wants them), with a switch to omit them (FDR 0021 §5 keeps batch/async out of a loop brief) |
| **call funnel** | `internal/proxy/proxy.go` `CallToolV1` → `callToolV1` ~977–1148 | statsd wrapper; `--expose` category gate; dynamic exclude set; `allow-tools` gate; builtin dispatch; `resolveToolName`; `status` of a failed server; ephemeral spawn-call-close; synthetic `resource-read` / `resource-templates`; child `tools/call`; result decode V0→V1 | the one place every call goes through. It **never** calls `permcheck.Resolver.Resolve` (moxy#485, test bb446c3). #486's enforce check slots in after the `allow-tools` gate |
| native dispatch | `internal/native/server.go` `handleToolsCall` ~215–277 | arg-key and enum validation, `stdin-param` extraction, **input blob substitution** (`substituteInputBlobURIs` → `/dev/fd/N`), argv build, `runMoxinProcess` (spool tee, process group), result shaping (`buildMCPResult` / `buildTextResult`), **output caching** to madder (`cacheAndGetURI`, `shouldCache`) | the blob-reference handling the embedder wants in process; coupled to `MadderBackend` (`internal/native/madder.go`, the in-process `blob_store_env` client) |
| permission resolution | `internal/permcheck/permcheck.go` | `Decision` (allow/ask/deny/unknown), `Resolver` built by walking `MOXIN_PATH` + system dir (`NewResolver` reads the **env**), `Resolve(ctx, "server.tool", args, cwd)`, dynamic predicates via `native.EvalDynamicPermsInDir` (exit 0/1/2; timeout, spawn failure and unmapped exits collapse to **ask**) | the policy half of the surface; needs a constructor that takes the moxin set already discovered instead of re-walking the env |
| hook (Claude Code) | `internal/hook/hook.go` `Handle` → `decidePreToolUse`, `permissionRequestDecisionUnderDontAsk` | per-event short-lived process; async/batch inner-tool peek (FDR 0011), builtin auto-allow, `tryPermsDecision`, dontAsk ask→deny (#484); opens `~/.local/log/moxy/hook.log` at **package init** | stays a CLI adapter: the hook *protocol* is Claude Code's. Its decision logic must be the same resolver and the same ask→deny mapping the enforce mode uses |
| batch / async | `internal/proxy/batch.go` `HandleBatch`; `internal/proxy/async.go` `HandleAsync`, `asyncEligible`, `handleBatchAsync` | preflight through the resolver (batch: allow/ask proceed; async: only deny and `permit-async=false` reject, FDR 0011), then dispatch back through `CallToolV1` | the detached job calls the funnel again later, so an enforce check in the funnel is re-run with no client attached — the "consent must travel with the job" cost (#486) |
| streamable HTTP | `internal/streamhttp/dispatch.go` `handleToolsCall` → `d.tools.CallToolV1`; `server.go` sessions, SSE, `/clown/system-prompt`, `/clown/exclude-tools`, `StartupGate` | pure transport over the `server.ToolProviderV1` / `ResourceProviderV1` / `PromptProviderV1` interfaces | CLI-side transport; it is what krone runs and must not change shape |
| stdio | `cmd/moxy/main.go` ~790–818 | go-mcp `server.New(transport.NewStdio(...), Options{Tools: p, Resources: p, Prompts: p})` | same: transport over the provider interfaces |
| child supervision | `internal/mcpclient/client.go` (`SpawnAndInitialize`, `readLoop`, `reapChild`, `childGoneError`, `Close`); `internal/proxy/proxy.go` `restartServer` ~1763 (make-before-break, probe before swap, #351/#405), `restartMoxin`, `Reload` ~1678 (full re-bootstrap, closes old children, `context.WithoutCancel`, #408), `ProbeEphemeral` | there is **no automatic respawn** of a child that dies: `Call` on a dead child returns `childGoneError`; recovery is the `restart` tool or `Reload` | the embedder needs `Restart(name)` / `Reload()` as methods plus a way to see the failed list; a watchdog is out of scope here |
| shutdown | `cmd/moxy/main.go` `SweepAsyncJobs` then `closeChildrenWithDeadline(children, 1s)` | interrupts async jobs so each writes a terminal journal record, then closes children with a deadline | becomes `Instance.Close` |
| process-wide side effects | `internal/proxy/proxy.go` `init` (opens `~/.local/log/moxy/debug.log`); `internal/hook/hook.go` `init`; `internal/stderrlog.Init` (redirects `os.Stderr`); `internal/statsd` (UDP, env-configured); `native.NewServer` reads `CLAUDE_SESSION_ID`; `permcheck.NewResolver` reads `MOXIN_PATH`; `pinnedParentReference`, `startupAllowTools` | acceptable in a binary, wrong in a library: an embedder must be able to construct two instances in one process (tests do) and must not have its stderr hijacked | each becomes lazy, injected, or per-instance; the CLI keeps today's defaults |

What the hook enforces and the server does not: `each-use`, `dynamic` and
`delegate-to-client` tiers are decided only in `moxy hook`; a plain
`tools/call` runs them (moxy#485). The skipped bats
`zz-tests_bats/server_side_perms.bats` (tag `server_perms`) encodes the
wanted server-side behaviour and is #486's acceptance test.

## Interface

### Package layout

Module path stays `code.linenisgreat.com/moxy`. Public packages live under
`pkg/`, the convention clown already uses for its one exported package
(`pkg/profilelookup`, clown#264). Anything not listed here stays under
`internal/`.

| Package | Today | Role |
| --- | --- | --- |
| `pkg/moxy` | new, built from `cmd/moxy/bootstrap.go`, `cmd/moxy/narrowing.go`'s serving half, and `runServer`'s wiring | the embedding facade: `Open`, `Instance` |
| `pkg/moxyfile` | `internal/config` (incl. `schema/` and the tommy codec) | the moxyfile model: `Config`, `ServerConfig`, `LoadHierarchy`, `LoadFileLiteral`, `Merge`, `ExpandHeaders`, `AllowToolSet`, the canonical writer behind `show-effective-moxyfile` |
| `pkg/narrowing` | `internal/narrowing` | `Check`, `CheckReload`, `Violation` (FDR 0013) |
| `pkg/perms` | `internal/permcheck` | `Decision`, `Resolver`, `Policy` (new, #486), `Resolve` |

Everything else stays internal and is reached only through `pkg/moxy`:
`proxy` (the engine), `native` (moxin dispatch, blob substitution, madder
client), `mcpclient`, `asyncjob`, `naming`, `toolfilter`, `toolexclude`,
`paginate`, `statsd`, `lifecyclelog`, `spoolctx`, `credentials`, `oauth`,
`stderrlog`, `streamhttp`, `hook`, `status`, `add`. Types from those that
appear in the facade's signatures are re-exported as aliases or wrapped
(`moxy.ServerSummary`, `moxy.BlobRef`); go-mcp's `protocol` types
(`ToolV1`, `ToolCallResultV1`, `ContentBlockV1`) are used as-is, since
go-mcp is already a public fleet module every consumer imports.

Rationale for four packages and not one: `moxyfile` and `narrowing` are
already consumed conceptually by launchers (`moxy narrows` is a CLI over
them; a spawner that renders a parent reference wants the writer in
process); `perms` is the object an embedder passes *in* and so must be
importable without pulling in the engine. Everything else is an
implementation detail of "make a tool call", and keeping it internal keeps
the compatibility promise small.

### CLI-only

- Transports: stdio (`moxy serve mcp`) and streamable HTTP (`moxy
  serve-http`, `internal/streamhttp`: sessions, SSE, `/healthz`,
  `/clown/system-prompt`, `/clown/exclude-tools`, the startup gate, the
  clown-plugin handshake line). The HTTP origin krone runs is this path and
  keeps its shape; both become `moxy.Open` + "hand the Instance to a
  transport" (`Instance` keeps implementing go-mcp's
  `ToolProviderV1` / `ResourceProviderV1` / `PromptProviderV1`).
- `moxy hook` and `install-mcp` / `generate-plugin` (Claude Code's hook
  protocol and plugin packaging).
- `status`, `add`, `narrows`, `show-effective-moxyfile`, `render`,
  `serve-moxin`, `version`.
- `stderrlog` (redirecting the process's stderr), the `debug.log` /
  `hook.log` files, statsd env configuration, `MOXY_HTTP_ADDR`,
  `MOXY_PARENT_MOXYFILE` and `MOXIN_PATH` *as environment variables*: the
  binary reads them and passes them to `Open` as options; the library never
  reads them itself.

### The facade

Shape, not final signatures; the Go doc of `pkg/moxy` is normative once it
exists.

```go
package moxy // code.linenisgreat.com/moxy/pkg/moxy

type Options struct {
    // Exactly one source of the effective moxyfile.
    Dir    string            // walk global → $HOME…dir → ./moxyfile, as serve does
    File   string            // one file plus its includes, nothing else
    Config *moxyfile.Config  // already loaded (an inline brief)

    // Narrowing (FDR 0013). Nil: no check. Pinned for the Instance's life;
    // every Reload is judged against it.
    Parent *moxyfile.Config

    MoxinPath       string   // "" → moxyfile(5) default hierarchy under Dir
    SystemMoxinDir  string   // "" → the build's bundled moxins
    SessionID       string   // cache scoping, spool naming, journal source

    Permissions perms.Policy // #486; zero value = Delegate (today's behaviour)

    NameTemplate string      // "" → "{server}.{tool}"
    Expose       string      // "" → all; else a toolfilter selector
    MetaTools    bool        // restart/batch/async/async-result/async-cancel/status

    Blobs   BlobStore        // nil → the default madder store, as the CLI uses
    Notify  func(*jsonrpc.Message) // tools/list_changed and forwarded child notifications
    Logger  *slog.Logger     // nil → discard; the CLI passes a stderr logger
    Connect ConnectFunc      // nil → stdio spawn + HTTP with the default credential store
}

func Open(ctx context.Context, o Options) (*Instance, error)

type Instance struct{ /* wraps *proxy.Proxy and its bootstrapper */ }

func (i *Instance) Tools(ctx context.Context) ([]protocol.ToolV1, error)
func (i *Instance) Call(ctx context.Context, c Call) (*Result, error)
func (i *Instance) Servers(ctx context.Context) []ServerSummary // running + failed
func (i *Instance) Restart(ctx context.Context, server string) error
func (i *Instance) Reload(ctx context.Context) error            // re-judges against Parent
func (i *Instance) Close(ctx context.Context) error             // sweep async jobs, close children

// go-mcp provider interfaces, so a transport is one line.
var _ server.ToolProviderV1     = (*Instance)(nil)
var _ server.ResourceProviderV1 = (*Instance)(nil)
var _ server.PromptProviderV1   = (*Instance)(nil)

type Call struct {
    Name   string          // rendered name, as tools/list advertised it
    Args   json.RawMessage
    Caller Caller
}

type Caller struct {
    ID     string          // opaque; recorded in the lifecycle log and statsd tags
    Cwd    string          // where dynamic predicates run (the hook's `cwd`)
    Policy *perms.Policy   // nil → Options.Permissions; may only narrow it
}

type Result struct {
    protocol.ToolCallResultV1
    Decision perms.Decision // what the funnel resolved, "" when not consulted
    Blobs    []BlobRef      // every madder://blobs/<digest> in the content, with store and size
}
```

`Call` is `CallToolV1` with a caller: the same funnel, the same gates in the
same order (`Expose`, dynamic exclude, `allow-tools`, then — under an enforce
policy — `perms.Resolve`), the same builtin, ephemeral, synthetic-resource
and child branches. `CallToolV1` itself remains, for the transports, as
`Call` with an empty `Caller`. `Decision` and `Blobs` are the two things the
MCP wire cannot carry and the embedder asked for.

Blob references: results already carry `madder://blobs/<digest>` URIs for
over-threshold or `cache-results = "always"` outputs; `Result.Blobs` lists
them structurally and `Instance.Blobs()` returns the `BlobStore` so the
embedder can read them without shelling out to `madder`. Input substitution
(a `madder://` URI passed as an argument becomes `/dev/fd/N` for the moxin
process) is unchanged and happens inside the call.

### Lifecycle contract for a long-lived embedder

1. **Open** loads and judges the config *before* anything is spawned
   (today's ordering in `bootstrap`: discovery, then `refuseUnlessNarrows`,
   then spawn). A widening against `Parent` is an error from `Open`, and
   nothing has started. Children that fail to start are not an error from
   `Open`; they appear in `Servers` as failed and their `<name>.status` tool
   reports why — the behaviour the CLI has today.
2. **Tools** reflects the live set: children, moxins, ephemeral metadata,
   meta tools if enabled, after `Expose`, the exclude set and `allow-tools`.
   The embedder may call it once and cache; `Notify` carries
   `tools/list_changed` when `Restart`, `Reload` or the exclude set changes
   it.
3. **Call** is safe for concurrent use. A call to a child that has died
   returns a tool-level error naming the child (today's `childGoneError`),
   never a panic and never a hang: child I/O is bounded by
   `MOXY_CHILD_MAX_MESSAGE` and the caller's `ctx`. Nothing respawns a
   child on its own; the embedder decides when to **Restart** it. A
   `Restart` is make-before-break (the old child serves until the new one
   has answered `tools/list`); a failed `Restart` leaves the old child in
   place.
4. **Reload** re-reads the moxyfile source (`Dir`/`File`; a `Config`
   source reloads to itself), re-discovers moxins, re-judges against the
   pinned `Parent` **and** the startup `allow-tools` baseline (moxy#461),
   and wholesale-replaces the child set. Refusal leaves the running set
   untouched and returns the violations.
5. **Async jobs** (if `MetaTools`) outlive the call that dispatched them and
   call the funnel again from a detached goroutine. The policy and caller
   recorded at dispatch travel with the job (this is where #486's "consent
   must travel with the job" is settled: the job re-enters the funnel with
   the dispatching `Caller`, and the funnel does not re-ask what it already
   resolved).
6. **Close** sweeps async jobs to a terminal journal state first, then
   closes children with a deadline. After `Close`, every method returns
   `ErrClosed`. Two instances in one process are independent: no package
   globals, no shared pinned parent, no shared log file.

### Permission policy (#486, settable here)

`perms.Policy` is the object #486 adds; this record fixes only its place:

```go
type Mode string
const (
    Delegate Mode = "delegate" // the funnel does not resolve; a hook in front decides (default)
    Enforce  Mode = "enforce"  // the funnel resolves; allow runs; ask, deny, unknown are refused
)
type Policy struct {
    Mode    Mode
    // further knobs per #486 (e.g. how Unknown maps) are #486's to define
}
```

It is reachable three ways, all ending in the same field on the funnel:
`Options.Permissions` for an embedder, a moxyfile key and/or serve flag for
the binary, and `Caller.Policy` per call, which may only narrow (a
`Delegate` instance can be told `Enforce` for one call; an `Enforce`
instance cannot be told `Delegate`). Narrowing (FDR 0013) gets a rule so a
child cannot widen the mode its spawner set. The refusal is an `isError`
tool result with a reason in the same shape as the hook's dontAsk denial
(`dontAskDenyReason`, #484), not a JSON-RPC error — what clown FDR 0021
§3.3 asks for.

### Versioning and consumption

- **One version.** `version.env`'s `MOXY_VERSION` (0.6.46 today) is the
  version of the binary *and* of the library; `moxy.Version()` returns the
  embedded value. The module is pre-1.0, so the Go-semver promise is "a
  minor bump may break `pkg/`"; the record of what changed is the changelog
  the `release` recipe writes. A compatibility tier line in each `pkg/*`
  doc comment states what is stable.
- **No tag-based Go resolution.** The fleet does not fetch modules by
  version; fleet modules are bridged through flake inputs (igloo RFC 0001,
  godyn(7) GO.NIX MANIFEST: a `go.nix` `flakeInputs` entry resolves to
  `inputs.<input>.packages.<system>.go-pkgs`, and "no version is stored").
  So the pin a consumer holds is the flake lock's revision of moxy; bumping
  the library is `nix flake update moxy` on the consumer's side.
- **moxy becomes a producer.** `flake.nix` gains `packages.go-pkgs` and
  `packages.go-pkgs-test` from `pkgs.mkGoPkgs { src = self; manifest =
  ./go.nix; inherit inputs; }` — the same call clown's flake makes for
  `pkg/profilelookup` (read in `~/eng/repos/clown/flake.nix` ~318–333).
  Today moxy's flake has **no** `go-pkgs` output (verified: the only match
  in `flake.nix` is a comment).
- **clown consumes it** by adding a `moxy` flake input and
  `"code.linenisgreat.com/moxy" = { input = "moxy"; }` to its `go.nix`
  `flakeInputs` (clown is on godyn `go.nix` since clown#249, closed). Then
  `import "code.linenisgreat.com/moxy/pkg/moxy"` just works.
  moxy's own fleet `flakeInputs` (`madder`, `purse-first`, `tommy`) ride
  along: `nix eval .#go-pkgs.passthru.goFlakeInputs` (recipe
  `debug-go-pkgs-flake-inputs`) shows the three resolved producer trees,
  which a consumer's bridge unions transitively per mkGoPkgs(7) ARGUMENTS,
  so clown declares only `moxy`. UNVERIFIED end to end until clown's flake
  actually evaluates with the bridge in place.
  UNVERIFIED and worth checking before step 2: moxy's `go.nix` pins
  `code.linenisgreat.com/purse-first/libs/dewey` as a versioned `require`
  (v0.5.0) while clown bridges the same module as a `flakeInput`; how godyn
  reconciles a module that is a require on one side and a flake input on
  the other is not described in godyn(7)'s sections I could read.
- **juggler** today lives inside clown (`internal/jugglerrun`,
  `internal/jugglertools`, `internal/jugglerloop`), so it consumes moxy
  through clown's `go.nix`. If clown#258 moves it to its own module, that
  module repeats the same two lines. UNVERIFIED: the state of clown#258.

### What the transports become

`runServer` shrinks to: parse flags, `moxy.Open` with the env-derived
options (`Dir: cwd`, `Parent` from `MOXY_PARENT_MOXYFILE`, `MoxinPath` from
`MOXIN_PATH`, `SessionID` from the existing fallback chain, `MetaTools:
true`, `Logger` on stderr), then either go-mcp's stdio server or
`streamhttp.New` over the `Instance`, then `Close`. `moxy hook` imports
`pkg/perms` for the resolver it already uses. Nothing a client sees changes:
tool names, descriptions, the `[perms: …]` suffix, resource URIs, the
handshake line, `/clown/*`, exit codes.

## Examples

A headless loop embedding moxy for one brief (clown FDR 0021 §4.3 shape):

```go
cfg, _ := moxyfile.ParseString(brief.Moxyfile)
parent, _ := moxyfile.LoadFileLiteral(spawnerReference)
inst, err := moxy.Open(ctx, moxy.Options{
    Config:      cfg,
    Parent:      &parent.Merged,
    MoxinPath:   unitMoxinPath,
    SessionID:   principal,
    Permissions: perms.Policy{Mode: perms.Enforce},
    NameTemplate: "{server}_{tool}",
})
if err != nil { /* widened the spawner, or unreadable: nothing was started */ }
defer inst.Close(ctx)

tools, _ := inst.Tools(ctx)          // offered to the model
res, err := inst.Call(ctx, moxy.Call{
    Name: "folio_read", Args: args,
    Caller: moxy.Caller{ID: principal, Cwd: worktree},
})
// err: transport-class failure (the run's problem)
// res.IsError && res.Decision == perms.Deny: refused by policy (the model's problem)
for _, b := range res.Blobs { /* madder://blobs/<digest>, readable via inst.Blobs() */ }
```

The binary, after the refactor, is the same thing with a transport:

```go
inst, err := moxy.Open(ctx, optionsFromEnvAndFlags())
srv, _ := server.New(transport.NewStdio(os.Stdin, os.Stdout), server.Options{
    Tools: inst, Resources: inst, Prompts: inst, Instructions: inst.Instructions(),
})
err = srv.Run(ctx)
inst.Close(ctx)
```

A test of the public surface, with no CLI and no hook:

```go
inst, _ := moxy.Open(ctx, moxy.Options{Dir: fixtureDir, MoxinPath: fixtureMoxins,
    Permissions: perms.Policy{Mode: perms.Enforce}})
res, _ := inst.Call(ctx, moxy.Call{Name: "permfix.asky"})
// res.IsError == true, res.Decision == perms.Ask, output does not contain "ran each-use"
```

## Migration steps

Each step is one merge; every existing Go and bats test stays green and the
CLI's observable behaviour is unchanged at every step. History stays
readable because moves are `git mv` with import rewrites in the same
commit and no behaviour change in a move commit.

1. **Producer + skeleton.** `packages.go-pkgs` / `go-pkgs-test` via
   `mkGoPkgs`; `pkg/moxy/doc.go` with `Version()`; `just build-go-pkgs`
   and `just debug-go-pkgs-flake-inputs`. Settles the transitive-inputs
   question above before any code moves.
2. **`internal/config` → `pkg/moxyfile`**, `internal/narrowing` →
   `pkg/narrowing`. Pure moves. The tommy `//go:generate` and
   `codemod-generate` follow the package.
3. **`internal/permcheck` → `pkg/perms`**, with a constructor that takes
   discovered moxin configs (`NewResolverFromConfigs`) beside the
   env-walking one the hook keeps using. `Policy` type added with
   `Delegate` only; nothing consults it yet.
4. **Process globals out of the engine.** `proxy`'s `init` log file and
   `native`'s `CLAUDE_SESSION_ID` read become options; `pinnedParentReference`
   and `startupAllowTools` move from package globals in `cmd/moxy` into a
   `serving` value owned by what will be the `Instance`. The CLI keeps its
   defaults.
5. **`pkg/moxy.Open`**: `cmd/moxy/bootstrap.go` and the serving half of
   `cmd/moxy/narrowing.go` move under `pkg/moxy`; `runServer` constructs an
   `Instance` and wires a transport. Meta tools registered inside `Open`
   under `MetaTools`.
6. **`Call`, `Caller`, `Result`, `Close`, `Restart`, `Reload`, `Servers`**
   plus Go tests under `pkg/moxy` driving a fixture moxin through the public
   surface (the test above, minus the enforce assertion).
7. **#486** on this surface: `Policy.Mode = Enforce` consulted in the
   funnel after `allow-tools`, the moxyfile key / flag, the narrowing rule,
   consent travelling with async jobs, the hook double-check question,
   `server_side_perms.bats` unskipped, moxy-serve-http(1) and moxin(7).
8. **clown consumes** (clown's lane, not this record's): flake input,
   `go.nix` line, `jugglerrun` gains an in-process `ToolPlaneLauncher` next
   to `ExecMoxy`.

## Limitations

- **The library is not a sandbox.** Embedding moves moxy, its child
  processes and the moxins' predicates into the embedder's process and
  scope. clown FDR 0021 §3.3 says this plainly and accepts it because the
  unit, not moxy, is the boundary; this record repeats it: an embedder
  that is not confined gains nothing from `Enforce` against a hostile model
  that can call the moxin binaries directly.
- **No automatic child respawn.** Supervision is `Restart` / `Reload` on
  request, as today. A watchdog, backoff, or health probing is a separate
  feature.
- **`Config` sources do not reload from disk.** `Reload` on an inline
  `Config` re-discovers moxins and re-judges but reads no file; an embedder
  that wants file semantics passes `File` or `Dir`.
- **go-mcp's provider interfaces are the transport boundary.** The library
  does not ship a transport; stdio and streamable HTTP stay in the binary.
  An embedder that wants to *serve* moxy to another process uses the binary
  or go-mcp's server packages directly over an `Instance`.
- **Dynamic predicates still fork.** `perms.Resolve` on a `dynamic` tool
  runs the moxin's predicate as a subprocess with a 2 s default timeout;
  in-process predicates are not in scope.
- **madder stays in-process and version-bound** (CLAUDE.md "madder is
  linked in-process"): an embedder's flake lock must carry a madder at
  least as new as the stores it will read, exactly as the binary's must.
- **Resources and prompts keep today's semantics** (`allow-tools` governs
  tools only, FDR 0013 Limitations); the facade exposes them unchanged.
- **Statsd is per process**, not per instance; two instances in one process
  share the emitter. Acceptable for the embedders in view.

## Tuning Levers

| Lever | Current | Rationale | Change signal |
|---|---|---|---|
| public package count | 4 (`moxy`, `moxyfile`, `narrowing`, `perms`) | smallest set whose types appear in the facade's signatures or are used by launchers in process | a consumer imports an `internal` type through a re-export more than once |
| `Close` child deadline | 1 s (today's `closeChildrenWithDeadline`) | clown grants 5 s SIGTERM→SIGKILL; 3 s HTTP drain + 1 s children fits | an embedder with no SIGKILL budget wants to wait; make it an option |
| meta tools in the library | opt-in (`MetaTools`) | FDR 0021 §5 keeps batch/async out of a loop brief; restart is a supervision verb the embedder has as a method | an embedder asks for async with a journal of its own |

## More Information

- moxy#487 (this record), moxy#485 (no server-side enforcement; the
  skipped `server_perms` bats), moxy#486 (enforce mode, lands on this
  surface), moxy#484 (dontAsk hook decisions, 18ea283).
- FDR 0004 (async dispatch), FDR 0011 (dispatch-time consent; the
  two-process hook/server split this refactor does not remove), FDR 0013
  (narrowing; `Parent` and the reload baseline).
- clown FDR 0021 §3.3 ("Embed (moxy#487) or call over MCP?"), §4.3 (tool
  plane inside the unit), Q13; clown `internal/jugglerrun/toolplane.go`
  (`ToolPlaneLauncher`, `ExecMoxy`) and `internal/jugglertools` (the MCP
  client the in-process executor replaces).
- igloo RFC 0001 (`go-pkgs` producer convention), godyn(7) GO.NIX MANIFEST;
  clown#249 (clown on godyn `go.nix`), clown#258 (juggler extraction),
  clown#264 (`pkg/profilelookup`, the precedent for `pkg/`).
