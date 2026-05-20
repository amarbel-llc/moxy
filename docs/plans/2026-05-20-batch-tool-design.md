# `batch` meta-tool — design

Issue: [#258](https://github.com/amarbel-llc/moxy/issues/258)

## Problem

Permission-gated tool calls fan out into per-item approval prompts when an
agent runs a known, fixed list of similar operations. The motivating case
in the issue: deleting 25 upstream `v*` tags via `grit.tag` burned 25
prompts to perform what is conceptually one operation.

Recurring shape:

- batch-deleting branches / tags
- removing a list of files
- creating several similar issues
- applying a known-good set of edits via `folio.write`
- closing a stack of stale PRs

Each is a fan-out over a small, known list where the per-item cost of
approval dwarfs the per-item work.

## Goals

1. Expose a single `batch` builtin tool that accepts an array of sub-calls
   and dispatches them through the existing `Proxy.CallToolV1` path.
2. Resolve sub-call permissions via moxy's own permission machinery (the
   same `perms-request` / `[dynamic-perms]` logic the PreToolUse hook
   uses today), so a single outer client prompt covers everything.
3. Refuse to bypass `deny` decisions or to silently approve non-moxin
   tools (builtins, external child MCP servers).
4. Return per-sub-call results in a stable, well-known schema that mirrors
   the `amarbel-llc/tap` NDJSON output mode so downstream tooling can
   reuse the same parsers.

## Non-goals

- Parallel execution. Sequential only in v1. The issue suggests both;
  sequential covers the named use cases and avoids worker-pool/ordering
  complexity.
- Nested batch calls. Flat list keeps the approval UI simple.
- Streaming partial results. Whole NDJSON document returned in one
  `ToolCallResultV1`.
- Cross-client permission introspection. Moxy does not read Claude
  Code's `settings.json`. Moxy decides on the basis of moxin
  `perms-request` only.
- madder-blob per-sub-call result formatter. The result-formatter
  abstraction is in place but only the JSON-array formatter ships in v1.
- Bats coverage lane as a CI gate. The flake output exists as a one-shot
  baseline tool for the refactor; promoting it to CI is a follow-up.

## Wire shape

### Input

```json
{
  "calls": [
    {"tool": "grit.tag", "args": {"subcommand": "delete", "name": "v1.13.0"}},
    {"tool": "grit.tag", "args": {"subcommand": "delete", "name": "v1.12.0"}}
  ],
  "on_error": "stop"
}
```

`on_error` defaults to `"stop"`. The only other value is `"continue"`.

### Output — NDJSON in a single text block

Schema mirrors `amarbel-llc/tap`'s `pkgs/ndjson` records, but we define
mirror types locally to avoid the module dep. Field names and JSON tags
match exactly so the two are wire-compatible.

Happy path:

```ndjson
{"type":"test","n":1,"description":"grit.tag","ok":true,"directive":null,"diagnostic":{"tool":"grit.tag","args":{"subcommand":"delete","name":"v1.13.0"}},"output":"<sub-call content>","subtest":[],"line":1}
{"type":"test","n":2,"description":"grit.tag","ok":true,"directive":null,"diagnostic":{"tool":"grit.tag","args":{"subcommand":"delete","name":"v1.12.0"}},"output":"<sub-call content>","subtest":[],"line":2}
{"type":"summary","passed":2,"failed":0,"skipped":0,"todo":0,"total":2,"plan_count":2,"bailed":false,"valid":true,"diagnostics":[]}
```

Pre-flight rejection (any sub-call resolves to `deny` or `unknown`):

```ndjson
{"type":"bailout","message":"batch denied: 2 of 5 sub-calls failed pre-flight","line":0}
{"type":"summary","passed":0,"failed":0,"skipped":5,"todo":0,"total":5,"plan_count":5,"bailed":true,"valid":false,"diagnostics":[{"line":3,"severity":"error","message":"slack.send_message: no moxin perm-request (deny by default)"},{"line":5,"severity":"error","message":"grit.push: dynamic-perms denied (refusing force-push to master)"}]}
```

Mid-batch failure with `on_error: "stop"`:

```ndjson
{"type":"test","n":1,"description":"grit.tag","ok":true,"diagnostic":{...},"output":"...","subtest":[],"line":1}
{"type":"test","n":2,"description":"grit.tag","ok":false,"diagnostic":{"error":"branch not found","kind":"tool"},"output":null,"subtest":[],"line":2}
{"type":"test","n":3,"description":"grit.tag","ok":false,"directive":{"kind":"skip","reason":"batch aborted: stopped at #2"},"diagnostic":null,"output":null,"subtest":[],"line":3}
{"type":"summary","passed":1,"failed":1,"skipped":1,"total":3,"plan_count":3,"bailed":true,"valid":true,"diagnostics":[]}
```

### MCP `IsError` flag

`IsError: true` iff `failed > 0` or `bailed = true`. Pure happy path is
`IsError: false`.

## Permission model

Moxy is the executor for moxin tools, so moxy decides. Claude Code's
`settings.json` is not consulted. The bulk call itself declares
`DestructiveHint: true` so the client prompts once when it appears.

Resolution per sub-call (mirrors `internal/hook`'s `tryPermsDecision`):

| `perms-request`       | Decision     |
|-----------------------|--------------|
| `always-allow`        | `Allow`      |
| `each-use`            | `Ask`        |
| `dynamic`             | run script   |
| `delegate-to-client`  | `Unknown`    |
| (missing / non-moxin) | `Unknown`    |

`dynamic` runs the `[dynamic-perms]` script with the same stdin shape
the PreToolUse hook uses. Script output `"allow"` / `"ask"` / `"deny"`
maps directly; empty output → `Unknown`; script crash → `Unknown` with
error reason.

A sub-call resolving to `Allow` or `Ask` proceeds inside the batch
without re-prompting (this is the whole value proposition). `Deny`
or `Unknown` aborts the entire batch with a bailout record. The
`Unknown` case is the deny-by-default boundary against builtins,
external MCP children, and any tool without an explicit moxy perm-rule.

## Architecture

### `internal/permcheck` (new)

Extracted from `internal/hook`. Pure decision package, no claude-code
JSON shape leakage.

```go
package permcheck

type Decision string

const (
    Allow   Decision = "allow"
    Ask     Decision = "ask"
    Deny    Decision = "deny"
    Unknown Decision = ""
)

type Resolver struct {
    perms map[string]toolPermInfo // "server.tool" -> info
}

func NewResolver() (*Resolver, error) // walks MOXIN_PATH

func (r *Resolver) Resolve(
    ctx context.Context,
    toolName string,        // "<server>.<tool>"
    args json.RawMessage,   // sub-call args, fed to dynamic-perms
    cwd string,
) (Decision, string)        // decision + human reason
```

`tryPermsDecision`, `discoverPermissions`, and `evalDynamicForHook`
move here. `internal/hook` becomes a thin adapter that translates a
`Resolver` decision into the JSON shape PreToolUse expects.

### `internal/proxy/batch.go` (new)

```go
type batchCall struct {
    Tool string          `json:"tool"`
    Args json.RawMessage `json:"args"`
}

type batchParams struct {
    Calls   []batchCall `json:"calls"`
    OnError string      `json:"on_error,omitempty"` // "stop" (default) | "continue"
}

func (p *Proxy) HandleBatch(
    ctx context.Context,
    args json.RawMessage,
) (*protocol.ToolCallResultV1, error)
```

`Proxy.resolver *permcheck.Resolver` is initialized in `NewProxy` (or
via setter) so it's available to `HandleBatch` and any future caller.

Execution outline:

```
HandleBatch(args)
  ├─ parse batchParams; validate calls, on_error
  ├─ for each call: p.resolver.Resolve(...)
  │     accumulate rejected / accepted lists
  ├─ if len(rejected) > 0 → emit bailout + summary, return IsError=true
  ├─ for each accepted (sequential):
  │     result, err := p.CallToolV1(ctx, tool, args)
  │     emit TestRecord
  │     if err or result.IsError:
  │         if on_error=stop → emit skip TestRecords for remainder, break
  │         else → continue
  ├─ emit summary
  └─ formatter.Format(records) → single ToolCallResultV1
```

Each successful sub-call invokes `p.maybeAdvanceStage` exactly as a
direct `CallToolV1` would. Batch is not a paved-path black hole.

### Result formatter seam

```go
type batchResultFormatter interface {
    Format(records []ndjsonRecord) (*protocol.ToolCallResultV1, error)
}
```

v1 ships with `ndjsonFormatter` producing a single text block. A
follow-up `madderBlobFormatter` will fan each sub-result to a blob and
return a JSON summary referencing the URIs; the seam keeps that change
to one file.

### `cmd/moxy/main.go` registration

Append after the existing `restart` registration:

```go
builtinRegistry.Register(
    protocol.ToolV1{
        Name:        "batch",
        Description: "...",
        InputSchema: ...,
        Annotations: &protocol.ToolAnnotations{
            DestructiveHint: boolPtr(true),
        },
    },
    p.HandleBatch,
)
```

### Local NDJSON mirror types

```go
package proxy // or a new internal/batchresult

type TestRecord struct {
    Type        string          `json:"type"`        // "test"
    N           int             `json:"n"`
    Description string          `json:"description"`
    OK          bool            `json:"ok"`
    Directive   *DirectiveValue `json:"directive"`
    Diagnostic  map[string]any  `json:"diagnostic"`
    Output      *string         `json:"output"`
    Subtest     []TestRecord    `json:"subtest"`     // always empty in v1
    Line        int             `json:"line"`
}

type DirectiveValue struct {
    Kind   string `json:"kind"`   // "skip" | "todo"
    Reason string `json:"reason"`
}

type BailoutRecord struct {
    Type    string `json:"type"`    // "bailout"
    Message string `json:"message"`
    Line    int    `json:"line"`
}

type SummaryRecord struct {
    Type        string              `json:"type"` // "summary"
    Passed      int                 `json:"passed"`
    Failed      int                 `json:"failed"`
    Skipped     int                 `json:"skipped"`
    Todo        int                 `json:"todo"`
    Total       int                 `json:"total"`
    PlanCount   int                 `json:"plan_count"`
    Bailed      bool                `json:"bailed"`
    Valid       bool                `json:"valid"`
    Diagnostics []SummaryDiagnostic `json:"diagnostics"`
}

type SummaryDiagnostic struct {
    Line     int    `json:"line"`
    Severity string `json:"severity"`
    Message  string `json:"message"`
}
```

Field names, JSON tags, and semantics match `amarbel-llc/tap`'s
`pkgs/ndjson` types exactly. No module dep.

## Error handling

Three tiers by failure location:

**Tier 1 — outer batch call (Go error result, no NDJSON):**
malformed args JSON, empty `calls`, invalid `on_error`.

**Tier 2 — pre-flight rejection (NDJSON bailout):**
any sub-call resolves to `deny` or `unknown`. All sub-calls reported
in `summary.diagnostics`. No sub-call runs.

**Tier 3 — per-sub-call failure (NDJSON test record `ok=false`):**
sub-call executes and fails. Two flavors:
- `diagnostic.kind = "transport"` — `CallToolV1` returns a Go error
- `diagnostic.kind = "tool"` — result has `IsError=true`

`on_error="stop"` emits skip TestRecords (`directive={kind:skip}`) for
the remainder. `on_error="continue"` keeps going.

**Context cancellation:** in-flight sub-call's transport error
propagates as Tier 3; no further sub-calls run regardless of
`on_error`; trailing bailout record explains why.

## Testing strategy

Coverage-first refactor.

### Pre-refactor: backfill unit tests

`internal/hook` resolver functions are at 0.0% unit coverage today
(`just cover-go ./internal/hook/...`). Indirect bats coverage exists
but isn't precise enough to confirm a safe extraction. Add unit tests
covering `tryPermsDecision`, `discoverPermissions`, `evalDynamicForHook`
in their current location, targeting ≥80% per-function. Tests move with
the code in the next step.

### Refactor: extract `internal/permcheck`

Move resolver functions and their tests. Re-run `just cover-go
./internal/permcheck/...` — coverage stays ≥80%. `internal/hook`
keeps a thin adapter calling `Resolver.Resolve`.

### New: `internal/permcheck` unit tests

- `TestResolve_AlwaysAllow` — `Allow`
- `TestResolve_EachUse` — `Ask`
- `TestResolve_DynamicAllow` / `_Ask` / `_Deny` — fixture moxin with
  `[dynamic-perms]` script returning each decision
- `TestResolve_DynamicEmpty` — empty script output → `Unknown`
- `TestResolve_DynamicScriptCrash` — script exits non-zero → `Unknown`
- `TestResolve_DynamicScriptTimeout` — script hangs → `Unknown`
- `TestResolve_NoMoxinForTool` → `Unknown`
- `TestResolve_InvalidToolName` → `Unknown`
- `TestNewResolver_EmptyMoxinPath` — empty resolver
- `TestNewResolver_MultipleMoxinDirs` — colon-separated `MOXIN_PATH`

Fixtures in `testdata/` mirroring `internal/native/config_test.go`'s
shape.

### New: `internal/proxy/batch_test.go`

Inject a tool-dispatch seam into `Proxy` so unit tests drive
`HandleBatch` against a scripted dispatcher (no real moxin
subprocesses).

- `TestBatch_ParsesArgs` — malformed JSON → Tier 1
- `TestBatch_EmptyCalls` — `calls:[]` → Tier 1
- `TestBatch_InvalidOnError` → Tier 1
- `TestBatch_AllAllow_Sequential` — three `always-allow` sub-calls
- `TestBatch_PreflightDeny_Aborts` — one sub-call denies
- `TestBatch_PreflightUnknown_Aborts` — non-moxin sub-call
- `TestBatch_PreflightCollectsAllRejections` — multiple rejections
  reported in one bailout
- `TestBatch_OnErrorStop` — failure mid-batch, skip directives on
  remainder
- `TestBatch_OnErrorContinue` — failure mid-batch, continues
- `TestBatch_TransportError` — `kind:"transport"`
- `TestBatch_ToolError` — `kind:"tool"`
- `TestBatch_ContextCancellation` — cancel mid-batch
- `TestBatch_PavedPathsStageAdvance` — stage advance per sub-call
- `TestBatch_NDJSONShape` — golden-file test for byte-for-byte schema

### New: `zz-tests_bats/batch.bats`

Tagged `# bats file_tags=batch,builtin`.

- batch sequential happy path
- batch pre-flight deny aborts
- batch on_error stop
- batch on_error continue

Fixture moxin needed: one deterministic-failing tool, one with
`dynamic-perms` returning decisions based on args. Reuse existing bats
lane wiring.

### Bats coverage lane (one-shot)

Add `bats-default-cover` flake output:

```nix
bats-default-cover = pkgs.buildGoCover {
  base = combined;
  coverIntegrationCommand = "<run mkBatsLane against $out/bin/moxy>";
};
```

Result is `$out/coverage.out` over the bats integration. Run once
before the refactor (baseline) and once after (regression check on
`hook`+`permcheck` combined). Not promoted to CI in this change.

### Coverage targets

| Package                              | Pre   | Must-meet (post) |
|--------------------------------------|-------|------------------|
| `internal/hook` resolver fns         | 0%    | n/a (moved)      |
| `internal/permcheck`                 | n/a   | ≥85%             |
| `internal/proxy` (batch.go)          | n/a   | ≥85%             |
| `hook`+`permcheck` combined via bats | (TBD) | ≥ baseline       |

## Rollback strategy

`batch` is purely additive: a new builtin tool registration, a new
package, and a refactor that's behavior-preserving for `internal/hook`.

**Dual-architecture period:** the refactor of `internal/hook` is the
only invasive change. During development, `permcheck` ships alongside
`hook`'s existing functions; `hook.Handle` switches to the new package
once `permcheck` unit tests pass. There is no flag — the refactor is a
straight extract, and the unit tests are the gate.

**Promotion criteria:** `batch.bats` passes in the nix sandbox lane;
`internal/permcheck` and `internal/proxy` batch unit tests pass; bats
coverage of `hook`+`permcheck` ≥ pre-refactor baseline.

**Rollback procedure:** revert the merge commit. The change set is
self-contained (new package + new builtin + thin adapter in `hook`)
so reverting restores prior behavior. No data migrations; no on-disk
state.

## Open questions deferred

- madder-blob result formatter — wired as a seam, implementation
  follow-up
- Parallel execution mode — explicit non-goal; revisit if a real
  workload exceeds sequential throughput
- Bats coverage lane in CI — separate plan
