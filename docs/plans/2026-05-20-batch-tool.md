# `batch` meta-tool implementation plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development to implement this plan task-by-task.

**Goal:** Add a `batch` builtin meta-tool that accepts an array of moxin sub-calls and dispatches them through `Proxy.CallToolV1` under a single outer client permission prompt. Moxy itself resolves each sub-call's permission via its own `perms-request` / `[dynamic-perms]` machinery — `deny` or `unknown` aborts the batch.

**Architecture:** Coverage-first refactor. (1) Backfill unit tests for the resolver code in `internal/hook`. (2) Extract the resolver to a new `internal/permcheck` package. (3) Build the `batch` builtin in `internal/proxy` on top of `permcheck`, emitting TAP-NDJSON-shaped output. (4) Register the builtin in `cmd/moxy/main.go`. (5) Add bats integration tests.

**Tech Stack:** Go 1.26, `purse-first/libs/go-mcp` (protocol types, `server.ToolRegistryV1`), `internal/native` (moxin config + dynamic-perms eval), bats for integration.

**Rollback:** Purely additive at the builtin layer. The `internal/hook` refactor is a straight extract behind unit-test coverage gates — if `permcheck` regresses bats coverage of `hook`+`permcheck` combined, revert the extract commit (the `batch` commits keep working against either layout because the public API is `Resolver.Resolve`).

**Design doc:** `docs/plans/2026-05-20-batch-tool-design.md`

**Issue:** [#258](https://github.com/amarbel-llc/moxy/issues/258)

---

## Workflow conventions

- Every task ends with a `commit` step. Do NOT batch multiple tasks into one commit.
- For Go compile checks during a task, prefer `hamster.go-build` over `just`. The merge-this-session hook will run `just` once at the end — don't run it per task.
- `just cover-go ./internal/<pkg>/...` is the per-package coverage shortcut (added in the design commit).
- Test names follow Go convention: `TestSubject_Behavior`.

---

## Phase 1 — Baseline coverage on the resolver

<!-- Captured 2026-05-20 by Task 1, against commit a8aa059
just cover-go ./internal/hook/...

  internal/hook total:                          43.4%
  internal/hook/hook.go:26  init                80.0%
  internal/hook/hook.go:49  logHookEvent         0.0%
  internal/hook/hook.go:85  debugHook            0.0%
  internal/hook/hook.go:125 matchMoxyPrefix    100.0%
  internal/hook/hook.go:151 Handle               0.0%
  internal/hook/hook.go:218 tryBuiltinAutoAllow 75.0%
  internal/hook/hook.go:241 tryPermsDecision     0.0%   ← extracts to permcheck
  internal/hook/hook.go:299 evalDynamicForHook  81.8%   ← extracts to permcheck
  internal/hook/hook.go:333 parseNativeToolName 90.9%
  internal/hook/hook.go:366 discoverPermissions  0.0%   ← extracts to permcheck
  internal/hook/hook.go:394 PluginDir            0.0%
  internal/hook/hook.go:415 InstallSettingsHook 84.6%

Phase 4 must keep internal/hook ≥ 43.4% combined with permcheck ≥ 85%.
-->

### Task 1: Capture pre-refactor coverage baseline

**Promotion criteria:** N/A — informational only.

**Files:**
- Read-only: `internal/hook/hook.go:241-390`

**Step 1: Run coverage on `internal/hook`**

Run: `just cover-go ./internal/hook/...`

Expected output includes:
```
code.linenisgreat.com/moxy/internal/hook/hook.go:241: tryPermsDecision     0.0%
code.linenisgreat.com/moxy/internal/hook/hook.go:366: discoverPermissions  0.0%
total: ... 43.4% of statements
```

**Step 2: Record the baseline in the plan**

Append a comment block to `docs/plans/2026-05-20-batch-tool.md` immediately under "Phase 1" recording the exact percentages from step 1's output, so Phase 4 has a comparison target.

**Step 3: Commit**

```bash
git add docs/plans/2026-05-20-batch-tool.md
git commit -m "docs(plans): record pre-refactor internal/hook coverage baseline"
```

---

### Task 2: Add unit tests for `discoverPermissions`

**Promotion criteria:** N/A.

**Files:**
- Modify: `internal/hook/hook_test.go`
- Create: `internal/hook/testdata/moxins-allow/_moxin.toml`
- Create: `internal/hook/testdata/moxins-allow/echo.toml`
- Create: `internal/hook/testdata/moxins-each/_moxin.toml`
- Create: `internal/hook/testdata/moxins-each/echo.toml`
- Create: `internal/hook/testdata/moxins-dynamic/_moxin.toml`
- Create: `internal/hook/testdata/moxins-dynamic/echo.toml`

**Step 1: Create fixture moxin trees**

Reference shape: `internal/native/config_test.go:198-211`.

`internal/hook/testdata/moxins-allow/_moxin.toml`:
```toml
schema = 1
name = "allow_srv"
description = "always-allow fixture"
```

`internal/hook/testdata/moxins-allow/echo.toml`:
```toml
schema = 1
command = "echo"
perms-request = "always-allow"
```

`internal/hook/testdata/moxins-each/_moxin.toml`:
```toml
schema = 1
name = "each_srv"
description = "each-use fixture"
```

`internal/hook/testdata/moxins-each/echo.toml`:
```toml
schema = 1
command = "echo"
perms-request = "each-use"
```

`internal/hook/testdata/moxins-dynamic/_moxin.toml`:
```toml
schema = 1
name = "dyn_srv"
description = "dynamic fixture"
```

`internal/hook/testdata/moxins-dynamic/echo.toml`:
```toml
schema = 1
command = "echo"
perms-request = "dynamic"

[dynamic-perms]
command = "true"
```

**Step 2: Write the failing tests**

Add to `internal/hook/hook_test.go` (after `TestEvalDynamicForHook`):

```go
func TestDiscoverPermissions_EmptyMoxinPath(t *testing.T) {
    t.Setenv("MOXIN_PATH", "")
    // SystemMoxinDir resolves from binary location; in tests it's the test
    // binary, which has no share/moxy/moxins/. Resolver should return empty.
    perms := discoverPermissions()
    if len(perms) != 0 {
        t.Fatalf("expected empty perms, got %d entries: %v", len(perms), perms)
    }
}

func TestDiscoverPermissions_SingleMoxin(t *testing.T) {
    t.Setenv("MOXIN_PATH", "testdata/moxins-allow")
    perms := discoverPermissions()
    info, ok := perms["allow_srv.echo"]
    if !ok {
        t.Fatalf("expected allow_srv.echo, got keys: %v", keysOf(perms))
    }
    if info.Perm != native.PermsAlwaysAllow {
        t.Fatalf("perm = %q, want %q", info.Perm, native.PermsAlwaysAllow)
    }
}

func TestDiscoverPermissions_MultipleMoxinDirs(t *testing.T) {
    t.Setenv("MOXIN_PATH", "testdata/moxins-allow:testdata/moxins-each")
    perms := discoverPermissions()
    if _, ok := perms["allow_srv.echo"]; !ok {
        t.Fatalf("missing allow_srv.echo; have keys: %v", keysOf(perms))
    }
    if _, ok := perms["each_srv.echo"]; !ok {
        t.Fatalf("missing each_srv.echo; have keys: %v", keysOf(perms))
    }
}

func TestDiscoverPermissions_DynamicCarriesSpec(t *testing.T) {
    t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic")
    perms := discoverPermissions()
    info, ok := perms["dyn_srv.echo"]
    if !ok {
        t.Fatalf("expected dyn_srv.echo, got keys: %v", keysOf(perms))
    }
    if info.Perm != native.PermsDynamic {
        t.Fatalf("perm = %q, want %q", info.Perm, native.PermsDynamic)
    }
    if info.DynamicPerms == nil {
        t.Fatal("expected DynamicPerms spec, got nil")
    }
    if info.DynamicPerms.Command != "true" {
        t.Fatalf("dynamic-perms command = %q, want %q", info.DynamicPerms.Command, "true")
    }
}

// keysOf returns the keys of a string-keyed map. Test-only helper.
func keysOf[V any](m map[string]V) []string {
    keys := make([]string, 0, len(m))
    for k := range m {
        keys = append(keys, k)
    }
    return keys
}
```

**Step 3: Run tests to verify they fail or pass**

Run: `MOXIN_PATH="" go test ./internal/hook/... -run TestDiscoverPermissions -v`

Expected: all four tests PASS (these are exercising existing behavior — they should pass on the first try, which is the point: they document what exists before we move it).

If any fail, that's a real bug — stop and ask before continuing.

**Step 4: Confirm coverage improved**

Run: `just cover-go ./internal/hook/...`

Expected: `discoverPermissions` now ≥85%. Record actual.

**Step 5: Commit**

```bash
git add internal/hook/hook_test.go internal/hook/testdata
git commit -m "test(hook): backfill discoverPermissions unit tests + fixtures"
```

---

### Task 3: Add unit tests for `tryPermsDecision`

**Promotion criteria:** N/A.

**Files:**
- Modify: `internal/hook/hook_test.go`

**Step 1: Write the failing tests**

Add to `internal/hook/hook_test.go`:

```go
func TestTryPermsDecision_AlwaysAllow(t *testing.T) {
    t.Setenv("MOXIN_PATH", "testdata/moxins-allow")
    var buf bytes.Buffer
    wrote := tryPermsDecision(
        "mcp__moxy__allow_srv_echo",
        "mcp__moxy__",
        map[string]any{},
        ".",
        &buf,
    )
    if !wrote {
        t.Fatal("expected tryPermsDecision to write a decision, got false")
    }
    var out hookOutput
    if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
        t.Fatalf("decode: %v; raw: %s", err, buf.String())
    }
    if got := out.HookSpecificOutput.PermissionDecision; got != "allow" {
        t.Fatalf("decision = %q, want %q", got, "allow")
    }
}

func TestTryPermsDecision_EachUse(t *testing.T) {
    t.Setenv("MOXIN_PATH", "testdata/moxins-each")
    var buf bytes.Buffer
    wrote := tryPermsDecision(
        "mcp__moxy__each_srv_echo",
        "mcp__moxy__",
        map[string]any{},
        ".",
        &buf,
    )
    if !wrote {
        t.Fatal("expected wrote=true")
    }
    var out hookOutput
    if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if got := out.HookSpecificOutput.PermissionDecision; got != "ask" {
        t.Fatalf("decision = %q, want %q", got, "ask")
    }
}

func TestTryPermsDecision_UnknownTool(t *testing.T) {
    t.Setenv("MOXIN_PATH", "testdata/moxins-allow")
    var buf bytes.Buffer
    wrote := tryPermsDecision(
        "mcp__moxy__unknown_srv_tool",
        "mcp__moxy__",
        map[string]any{},
        ".",
        &buf,
    )
    if wrote {
        t.Fatalf("expected fall-through (wrote=false), got wrote=true, buf=%q", buf.String())
    }
    if buf.Len() != 0 {
        t.Fatalf("expected empty buf on fall-through, got %q", buf.String())
    }
}

func TestTryPermsDecision_NonMoxyPrefix(t *testing.T) {
    var buf bytes.Buffer
    wrote := tryPermsDecision(
        "Read",
        "mcp__moxy__",
        map[string]any{},
        ".",
        &buf,
    )
    if wrote {
        t.Fatal("expected fall-through for non-prefixed tool name")
    }
}
```

**Step 2: Run tests**

Run: `MOXIN_PATH="" go test ./internal/hook/... -run TestTryPermsDecision -v`

Expected: PASS (documenting existing behavior).

**Step 3: Confirm coverage improved**

Run: `just cover-go ./internal/hook/...`

Expected: `tryPermsDecision` ≥75% (the dynamic branch is exercised by existing `TestEvalDynamicForHook`).

**Step 4: Commit**

```bash
git add internal/hook/hook_test.go
git commit -m "test(hook): backfill tryPermsDecision unit tests"
```

---

## Phase 2 — Extract `internal/permcheck`

### Task 4: Create `internal/permcheck` skeleton

**Promotion criteria:** Phase 3 builds on this package.

**Files:**
- Create: `internal/permcheck/permcheck.go`
- Create: `internal/permcheck/permcheck_test.go`

**Step 1: Write the package skeleton**

`internal/permcheck/permcheck.go`:

```go
// Package permcheck resolves moxin tool permission decisions.
//
// It mirrors the resolver previously embedded in internal/hook. The
// PreToolUse hook adapter and the proxy's batch meta-tool both call
// Resolver.Resolve to decide whether a given tool call is allowed,
// asked, denied, or unknown (deny-by-default for non-moxin tools).
package permcheck

import (
    "context"
    "encoding/json"
    "os"

    "code.linenisgreat.com/moxy/internal/native"
)

// Decision is the resolved permission outcome for one tool call.
type Decision string

const (
    Allow   Decision = "allow"
    Ask     Decision = "ask"
    Deny    Decision = "deny"
    Unknown Decision = "" // fall-through: tool has no moxin perm-request
)

// ToolPermInfo carries the resolver inputs for one tool.
type ToolPermInfo struct {
    Perm         native.PermsRequest
    DynamicPerms *native.DynamicPermsSpec
}

// Resolver caches the moxin perms map and resolves decisions per call.
type Resolver struct {
    perms map[string]ToolPermInfo
}

// NewResolver walks MOXIN_PATH (and the system moxin dir) once and
// caches every tool's perms-request. Tools without an explicit
// perms-request are omitted from the map.
func NewResolver() (*Resolver, error) {
    perms, err := discoverPermissions()
    if err != nil {
        return nil, err
    }
    return &Resolver{perms: perms}, nil
}

// Resolve returns the decision for toolName ("<server>.<tool>" form).
// args is the sub-call's JSON args, fed to dynamic-perms when relevant.
// cwd is the working directory the dynamic-perms script runs in.
func (r *Resolver) Resolve(
    ctx context.Context,
    toolName string,
    args json.RawMessage,
    cwd string,
) (Decision, string) {
    info, ok := r.perms[toolName]
    if !ok {
        return Unknown, "no moxin perm-request for " + toolName
    }
    switch info.Perm {
    case native.PermsAlwaysAllow:
        return Allow, "always-allow by moxin config"
    case native.PermsEachUse:
        return Ask, "each-use: requires explicit approval"
    case native.PermsDynamic:
        return evalDynamic(ctx, info.DynamicPerms, args, cwd)
    default:
        return Unknown, "delegate-to-client or unrecognized perms-request"
    }
}

// evalDynamic runs the per-tool dynamic-perms predicate and maps its
// decision into (Decision, reason).
func evalDynamic(
    ctx context.Context,
    spec *native.DynamicPermsSpec,
    args json.RawMessage,
    cwd string,
) (Decision, string) {
    if spec == nil {
        return Unknown, "dynamic-perms: no [dynamic-perms] spec on tool"
    }
    dec, reason := native.EvalDynamicPermsInDir(ctx, spec, nil, args, cwd)
    switch dec {
    case native.DynPermsAllow:
        return Allow, reason
    case native.DynPermsAsk:
        return Ask, reason
    case native.DynPermsDeny:
        return Deny, reason
    default:
        return Unknown, reason
    }
}

// discoverPermissions walks MOXIN_PATH and the system moxin dir, then
// returns a map of "server.tool" names to their perm info.
func discoverPermissions() (map[string]ToolPermInfo, error) {
    moxinPath := os.Getenv("MOXIN_PATH")
    systemDir := native.SystemMoxinDir()
    configs, err := native.DiscoverConfigs(moxinPath, systemDir)
    if err != nil {
        return nil, err
    }
    perms := make(map[string]ToolPermInfo)
    for _, cfg := range configs {
        for _, tool := range cfg.Tools {
            if tool.PermsRequest != "" {
                perms[cfg.Name+"."+tool.Name] = ToolPermInfo{
                    Perm:         tool.PermsRequest,
                    DynamicPerms: tool.DynamicPerms,
                }
            }
        }
    }
    return perms, nil
}
```

`internal/permcheck/permcheck_test.go` (minimal — just verify the package compiles and `NewResolver()` works on an empty env):

```go
package permcheck

import (
    "testing"
)

func TestNewResolver_EmptyMoxinPath(t *testing.T) {
    t.Setenv("MOXIN_PATH", "")
    r, err := NewResolver()
    if err != nil {
        t.Fatalf("NewResolver: %v", err)
    }
    if r == nil {
        t.Fatal("NewResolver returned nil resolver")
    }
}
```

**Step 2: Compile-check the new package**

Use `mcp__plugin_moxy_moxy__hamster_go-build` with packages `./internal/permcheck`.
Expected: no errors.

**Step 3: Run the smoke test**

Run: `MOXIN_PATH="" go test ./internal/permcheck/... -v`

Expected: `TestNewResolver_EmptyMoxinPath` PASS.

**Step 4: Commit**

```bash
git add internal/permcheck
git commit -m "feat(permcheck): scaffold resolver package extracted from internal/hook"
```

---

### Task 5: Port unit tests into `internal/permcheck`

**Promotion criteria:** Coverage of `permcheck` ≥85% before Task 6 wires it into `internal/hook`.

**Files:**
- Modify: `internal/permcheck/permcheck_test.go`
- Create: `internal/permcheck/testdata/moxins-allow/_moxin.toml`
- Create: `internal/permcheck/testdata/moxins-allow/echo.toml`
- Create: `internal/permcheck/testdata/moxins-each/_moxin.toml`
- Create: `internal/permcheck/testdata/moxins-each/echo.toml`
- Create: `internal/permcheck/testdata/moxins-dynamic-allow/_moxin.toml`
- Create: `internal/permcheck/testdata/moxins-dynamic-allow/echo.toml`
- Create: `internal/permcheck/testdata/moxins-dynamic-deny/_moxin.toml`
- Create: `internal/permcheck/testdata/moxins-dynamic-deny/echo.toml`

**Step 1: Copy fixtures from `internal/hook/testdata`**

Copy `internal/hook/testdata/moxins-allow/`, `moxins-each/`, and `moxins-dynamic/` into `internal/permcheck/testdata/`. Rename the dynamic fixture to `moxins-dynamic-allow` (its script always returns allow). Add a new `moxins-dynamic-deny` with a `[dynamic-perms]` script returning `deny`.

`internal/permcheck/testdata/moxins-dynamic-allow/echo.toml`:
```toml
schema = 1
command = "echo"
perms-request = "dynamic"

[dynamic-perms]
command = "sh"
args = ["-c", "echo allow"]
```

`internal/permcheck/testdata/moxins-dynamic-deny/_moxin.toml`:
```toml
schema = 1
name = "dyndeny_srv"
description = "dynamic deny fixture"
```

`internal/permcheck/testdata/moxins-dynamic-deny/echo.toml`:
```toml
schema = 1
command = "echo"
perms-request = "dynamic"

[dynamic-perms]
command = "sh"
args = ["-c", "echo deny"]
```

(Adjust the `[dynamic-perms]` exact shape if the existing `native.DynamicPermsSpec` expects different fields — check `internal/native/dynperms.go` once before writing.)

**Step 2: Write the tests**

Replace `internal/permcheck/permcheck_test.go` body:

```go
package permcheck

import (
    "context"
    "encoding/json"
    "testing"

    "code.linenisgreat.com/moxy/internal/native"
)

func TestNewResolver_EmptyMoxinPath(t *testing.T) {
    t.Setenv("MOXIN_PATH", "")
    r, err := NewResolver()
    if err != nil {
        t.Fatalf("NewResolver: %v", err)
    }
    dec, _ := r.Resolve(context.Background(), "anything.tool", nil, ".")
    if dec != Unknown {
        t.Fatalf("dec = %q, want %q", dec, Unknown)
    }
}

func TestResolve_AlwaysAllow(t *testing.T) {
    t.Setenv("MOXIN_PATH", "testdata/moxins-allow")
    r, err := NewResolver()
    if err != nil {
        t.Fatal(err)
    }
    dec, reason := r.Resolve(context.Background(), "allow_srv.echo", nil, ".")
    if dec != Allow {
        t.Fatalf("dec = %q, want %q (reason: %s)", dec, Allow, reason)
    }
}

func TestResolve_EachUse(t *testing.T) {
    t.Setenv("MOXIN_PATH", "testdata/moxins-each")
    r, _ := NewResolver()
    dec, _ := r.Resolve(context.Background(), "each_srv.echo", nil, ".")
    if dec != Ask {
        t.Fatalf("dec = %q, want %q", dec, Ask)
    }
}

func TestResolve_DynamicAllow(t *testing.T) {
    t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-allow")
    r, _ := NewResolver()
    dec, reason := r.Resolve(
        context.Background(),
        "dynallow_srv.echo",
        json.RawMessage(`{}`),
        ".",
    )
    if dec != Allow {
        t.Fatalf("dec = %q, want %q (reason: %s)", dec, Allow, reason)
    }
}

func TestResolve_DynamicDeny(t *testing.T) {
    t.Setenv("MOXIN_PATH", "testdata/moxins-dynamic-deny")
    r, _ := NewResolver()
    dec, reason := r.Resolve(
        context.Background(),
        "dyndeny_srv.echo",
        json.RawMessage(`{}`),
        ".",
    )
    if dec != Deny {
        t.Fatalf("dec = %q, want %q (reason: %s)", dec, Deny, reason)
    }
}

func TestResolve_UnknownTool(t *testing.T) {
    t.Setenv("MOXIN_PATH", "testdata/moxins-allow")
    r, _ := NewResolver()
    dec, _ := r.Resolve(context.Background(), "missing_srv.tool", nil, ".")
    if dec != Unknown {
        t.Fatalf("dec = %q, want %q", dec, Unknown)
    }
}

func TestResolve_DynamicNilSpec(t *testing.T) {
    r := &Resolver{
        perms: map[string]ToolPermInfo{
            "bad.tool": {Perm: native.PermsDynamic, DynamicPerms: nil},
        },
    }
    dec, _ := r.Resolve(context.Background(), "bad.tool", nil, ".")
    if dec != Unknown {
        t.Fatalf("dec = %q, want %q (defensive nil-spec branch)", dec, Unknown)
    }
}
```

**Step 3: Run tests**

Run: `MOXIN_PATH="" go test ./internal/permcheck/... -v`
Expected: all PASS.

**Step 4: Confirm coverage**

Run: `just cover-go ./internal/permcheck/...`
Expected: ≥85% on `Resolve`, `evalDynamic`, `discoverPermissions`.

**Step 5: Commit**

```bash
git add internal/permcheck
git commit -m "test(permcheck): port resolver tests + fixtures"
```

---

### Task 6: Swap `internal/hook` to use `permcheck`

**Promotion criteria:** All Phase 1 tests still pass against the adapter; remove the now-unused functions from `internal/hook` in a follow-on cleanup commit if you want to keep this commit minimal.

**Files:**
- Modify: `internal/hook/hook.go:241-390`
- Modify: `internal/hook/hook.go` (imports)

**Step 1: Add a package-level resolver and rewrite `tryPermsDecision` to delegate**

In `internal/hook/hook.go`, add a new import:

```go
"code.linenisgreat.com/moxy/internal/permcheck"
```

Add a package-level resolver var with lazy init (resolvers walk the filesystem, so we don't want to do it on import):

```go
var (
    permResolver     *permcheck.Resolver
    permResolverOnce sync.Once
    permResolverErr  error
)

func getResolver() (*permcheck.Resolver, error) {
    permResolverOnce.Do(func() {
        permResolver, permResolverErr = permcheck.NewResolver()
    })
    return permResolver, permResolverErr
}
```

Add `"sync"` to the imports.

Replace the body of `tryPermsDecision` (lines 241-294) with:

```go
func tryPermsDecision(toolName, prefix string, toolInput map[string]any, cwd string, w io.Writer) bool {
    serverTool, ok := parseNativeToolName(toolName, prefix)
    if !ok {
        return false
    }

    resolver, err := getResolver()
    if err != nil {
        debugHook("  getResolver error: %v", err)
        return false
    }

    args, err := json.Marshal(toolInput)
    if err != nil {
        debugHook("  re-marshal toolInput error: %v", err)
        return false
    }

    decision, reason := resolver.Resolve(context.Background(), serverTool, args, cwd)
    debugHook("  resolver decision=%q reason=%q", decision, reason)

    var decStr string
    switch decision {
    case permcheck.Allow:
        decStr = "allow"
    case permcheck.Ask:
        decStr = "ask"
    case permcheck.Deny:
        decStr = "deny"
    default:
        return false // Unknown → fall through to client
    }

    out := hookOutput{
        HookSpecificOutput: hookDecision{
            HookEventName:            "PreToolUse",
            PermissionDecision:       decStr,
            PermissionDecisionReason: reason,
        },
    }
    if err := json.NewEncoder(w).Encode(out); err != nil {
        log.Printf("hook: ignoring encode error (fail-open): %v", err)
        return false
    }
    return true
}
```

**Step 2: Delete now-unused functions**

Remove `evalDynamicForHook`, `toolPermInfo`, and `discoverPermissions` from `internal/hook/hook.go` (they've moved to `permcheck`). Keep `parseNativeToolName` — it's hook-specific (prefix-aware).

**Step 3: Compile and run the hook test suite**

Use `hamster.go-build` on `./internal/hook`. Expected: no errors.

Run: `MOXIN_PATH="" go test ./internal/hook/... -v`

Expected: all tests PASS. `TestDiscoverPermissions_*` and `TestEvalDynamicForHook` still need to compile against the new layout — if they reference the now-removed functions, **stop** and ask the user whether to:
(a) delete the now-redundant hook-level tests (they're duplicated in `permcheck` already), or
(b) keep them as adapter integration tests by rewriting them through `tryPermsDecision` only.

**Step 4: Coverage check on both packages**

Run:
```
just cover-go ./internal/hook/...
just cover-go ./internal/permcheck/...
```

Expected: `internal/hook` total still ≥ pre-refactor (43.4% baseline). `internal/permcheck` ≥85%.

If `internal/hook` regresses below baseline, the adapter has uncovered paths — add a unit test for the Unknown→fall-through case before committing.

**Step 5: Commit**

```bash
git add internal/hook/hook.go internal/hook/hook_test.go
git commit -m "refactor(hook): delegate permission resolution to internal/permcheck"
```

---

## Phase 3 — Build the `batch` builtin

### Task 7: Add NDJSON record types

**Promotion criteria:** Task 8 consumes these types.

**Files:**
- Create: `internal/proxy/batch_ndjson.go`
- Create: `internal/proxy/batch_ndjson_test.go`

**Step 1: Write the types**

`internal/proxy/batch_ndjson.go`:

```go
package proxy

// NDJSON mirror types matching amarbel-llc/tap pkgs/ndjson schema.
// Defined locally to avoid the module dep; field names and JSON tags
// match exactly so an amarbel-llc/tap consumer can json.Unmarshal
// batch output into their types.

type ndjsonTestRecord struct {
    Type        string             `json:"type"`        // "test"
    N           int                `json:"n"`           // 1-indexed
    Description string             `json:"description"` // tool name
    OK          bool               `json:"ok"`
    Directive   *ndjsonDirective   `json:"directive"`
    Diagnostic  map[string]any     `json:"diagnostic"`
    Output      *string            `json:"output"`
    Subtest     []ndjsonTestRecord `json:"subtest"`     // always empty in v1
    Line        int                `json:"line"`        // 1-indexed
}

type ndjsonDirective struct {
    Kind   string `json:"kind"`   // "skip" | "todo"
    Reason string `json:"reason"`
}

type ndjsonBailoutRecord struct {
    Type    string `json:"type"`    // "bailout"
    Message string `json:"message"`
    Line    int    `json:"line"`
}

type ndjsonSummaryRecord struct {
    Type        string                    `json:"type"` // "summary"
    Passed      int                       `json:"passed"`
    Failed      int                       `json:"failed"`
    Skipped     int                       `json:"skipped"`
    Todo        int                       `json:"todo"`
    Total       int                       `json:"total"`
    PlanCount   int                       `json:"plan_count"`
    Bailed      bool                      `json:"bailed"`
    Valid       bool                      `json:"valid"`
    Diagnostics []ndjsonSummaryDiagnostic `json:"diagnostics"`
}

type ndjsonSummaryDiagnostic struct {
    Line     int    `json:"line"`
    Severity string `json:"severity"`
    Message  string `json:"message"`
}
```

**Step 2: Write the schema-shape test**

`internal/proxy/batch_ndjson_test.go`:

```go
package proxy

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestNDJSONTestRecord_JSONTags(t *testing.T) {
    rec := ndjsonTestRecord{
        Type:        "test",
        N:           1,
        Description: "foo.bar",
        OK:          true,
        Diagnostic:  map[string]any{"tool": "foo.bar"},
        Subtest:     []ndjsonTestRecord{},
        Line:        1,
    }
    b, err := json.Marshal(rec)
    if err != nil {
        t.Fatal(err)
    }
    s := string(b)
    for _, want := range []string{
        `"type":"test"`,
        `"n":1`,
        `"description":"foo.bar"`,
        `"ok":true`,
        `"directive":null`,
        `"output":null`,
        `"subtest":[]`,
        `"line":1`,
    } {
        if !strings.Contains(s, want) {
            t.Errorf("missing %q in %s", want, s)
        }
    }
}

func TestNDJSONSummaryRecord_JSONTags(t *testing.T) {
    rec := ndjsonSummaryRecord{
        Type:        "summary",
        Passed:      2,
        Total:       2,
        PlanCount:   2,
        Valid:       true,
        Diagnostics: []ndjsonSummaryDiagnostic{},
    }
    b, err := json.Marshal(rec)
    if err != nil {
        t.Fatal(err)
    }
    s := string(b)
    for _, want := range []string{
        `"type":"summary"`,
        `"passed":2`,
        `"plan_count":2`,
        `"valid":true`,
        `"diagnostics":[]`,
    } {
        if !strings.Contains(s, want) {
            t.Errorf("missing %q in %s", want, s)
        }
    }
}
```

**Step 3: Run the test**

Run: `go test ./internal/proxy/... -run TestNDJSON -v`
Expected: PASS.

**Step 4: Commit**

```bash
git add internal/proxy/batch_ndjson.go internal/proxy/batch_ndjson_test.go
git commit -m "feat(proxy): add NDJSON mirror types for batch output"
```

---

### Task 8: Wire `permcheck.Resolver` into `Proxy`

**Promotion criteria:** Task 9 calls `p.resolver.Resolve`.

**Files:**
- Modify: `internal/proxy/proxy.go:110-170` (struct + setter)
- Modify: `cmd/moxy/main.go` (init the resolver, call setter)

**Step 1: Add field and setter to `Proxy`**

In `internal/proxy/proxy.go`, add import:
```go
"code.linenisgreat.com/moxy/internal/permcheck"
```

In the `Proxy` struct (line 110-129), add a field:
```go
resolver *permcheck.Resolver
```

After `SetBuiltinTools` (line 152-154), add:
```go
func (p *Proxy) SetResolver(r *permcheck.Resolver) {
    p.resolver = r
}
```

**Step 2: Init the resolver in `cmd/moxy/main.go`**

In `cmd/moxy/main.go`, locate where `p.SetBuiltinTools(builtinRegistry)` is called (~line 491). Just before it, add:

```go
resolver, err := permcheck.NewResolver()
if err != nil {
    fmt.Fprintf(os.Stderr, "moxy: building permission resolver: %v\n", err)
} else {
    p.SetResolver(resolver)
}
```

Add the import `"code.linenisgreat.com/moxy/internal/permcheck"`.

**Step 3: Compile**

Use `hamster.go-build` on `./...`. Expected: no errors.

**Step 4: Commit**

```bash
git add internal/proxy/proxy.go cmd/moxy/main.go
git commit -m "feat(proxy): wire permcheck.Resolver into Proxy via SetResolver"
```

---

### Task 9: Implement `HandleBatch` (happy path + pre-flight only)

**Promotion criteria:** Task 10 extends with on_error semantics; Task 11 wires it into the builtin registry.

**Files:**
- Create: `internal/proxy/batch.go`
- Create: `internal/proxy/batch_test.go`

**Step 1: Write the failing happy-path test**

`internal/proxy/batch_test.go`:

```go
package proxy

import (
    "context"
    "encoding/json"
    "strings"
    "testing"

    "code.linenisgreat.com/moxy/internal/permcheck"

    "github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// newProxyWithResolverAndDispatch builds a Proxy stub wired with a
// hand-crafted resolver and a scripted sub-call dispatcher.
func newProxyWithResolverAndDispatch(
    t *testing.T,
    perms map[string]permcheck.ToolPermInfo,
    dispatch func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error),
) *Proxy {
    t.Helper()
    p := &Proxy{}
    // permcheck.Resolver exposes no constructor for a pre-built perms
    // map, so use the package's test helper. If that helper doesn't
    // exist, add an exported NewResolverWithPerms(perms) constructor in
    // internal/permcheck — Task 9a.
    p.SetResolver(permcheck.NewResolverWithPerms(perms))
    p.dispatchSubCall = dispatch
    return p
}

func TestBatch_AllAllow_Sequential(t *testing.T) {
    okResult := &protocol.ToolCallResultV1{
        Content: []protocol.ContentBlockV1{{Type: "text", Text: "ok"}},
    }
    var calls []string
    p := newProxyWithResolverAndDispatch(t,
        map[string]permcheck.ToolPermInfo{
            "fake.tool": {Perm: nativePermsAlwaysAllow()},
        },
        func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
            calls = append(calls, name)
            return okResult, nil
        },
    )

    in := []byte(`{
        "calls": [
            {"tool": "fake.tool", "args": {"a": 1}},
            {"tool": "fake.tool", "args": {"a": 2}}
        ]
    }`)

    res, err := p.HandleBatch(context.Background(), in)
    if err != nil {
        t.Fatalf("HandleBatch: %v", err)
    }
    if res.IsError {
        t.Fatalf("expected IsError=false; content=%v", res.Content)
    }
    if got, want := len(calls), 2; got != want {
        t.Fatalf("dispatch invoked %d times, want %d", got, want)
    }
    body := res.Content[0].Text
    if !strings.Contains(body, `"type":"test"`) || !strings.Contains(body, `"type":"summary"`) {
        t.Fatalf("body missing expected records: %s", body)
    }
    if !strings.Contains(body, `"passed":2`) {
        t.Fatalf("expected passed=2 in summary: %s", body)
    }
}

// nativePermsAlwaysAllow is a tiny adapter so the test doesn't import
// internal/native directly. Replace with a direct import if more
// PermsRequest values are needed.
func nativePermsAlwaysAllow() native.PermsRequest {
    return native.PermsAlwaysAllow
}
```

(If the test wants to avoid the `internal/native` import, expose a `permcheck.NewToolPermInfoAllow()` constructor instead.)

**Step 2: Write `internal/proxy/batch.go` with just the happy path + pre-flight**

```go
package proxy

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"

    "code.linenisgreat.com/moxy/internal/permcheck"
    "github.com/amarbel-llc/purse-first/libs/go-mcp/protocol"
)

// batchCall is one entry in the batch.calls array.
type batchCall struct {
    Tool string          `json:"tool"`
    Args json.RawMessage `json:"args"`
}

// batchParams is the wire input shape for the batch builtin.
type batchParams struct {
    Calls   []batchCall `json:"calls"`
    OnError string      `json:"on_error,omitempty"`
}

// subCallDispatcher is the seam HandleBatch uses to invoke each
// sub-call. The default points at p.CallToolV1; tests override it.
type subCallDispatcher func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error)

// HandleBatch is the dispatch entrypoint for the `batch` builtin tool.
// See docs/plans/2026-05-20-batch-tool-design.md.
func (p *Proxy) HandleBatch(
    ctx context.Context,
    args json.RawMessage,
) (*protocol.ToolCallResultV1, error) {
    var params batchParams
    if err := json.Unmarshal(args, &params); err != nil {
        return protocol.ErrorResultV1(
            fmt.Sprintf("invalid batch args: %v", err),
        ), nil
    }
    if len(params.Calls) == 0 {
        return protocol.ErrorResultV1("batch.calls must be non-empty"), nil
    }
    onError := params.OnError
    if onError == "" {
        onError = "stop"
    }
    if onError != "stop" && onError != "continue" {
        return protocol.ErrorResultV1(
            fmt.Sprintf(`invalid on_error %q (want "stop" or "continue")`, onError),
        ), nil
    }

    if p.resolver == nil {
        return protocol.ErrorResultV1(
            "batch unavailable: no permission resolver configured",
        ), nil
    }

    // Pre-flight: resolve every sub-call.
    type rejection struct {
        index int
        call  batchCall
        dec   permcheck.Decision
        reason string
    }
    var rejected []rejection
    for i, c := range params.Calls {
        dec, reason := p.resolver.Resolve(ctx, c.Tool, c.Args, ".")
        if dec == permcheck.Allow || dec == permcheck.Ask {
            continue
        }
        rejected = append(rejected, rejection{index: i, call: c, dec: dec, reason: reason})
    }

    if len(rejected) > 0 {
        return emitPreflightBailout(params.Calls, rejected), nil
    }

    // Execute sub-calls sequentially.
    dispatch := p.dispatchSubCall
    if dispatch == nil {
        dispatch = p.CallToolV1
    }
    records := make([]ndjsonTestRecord, 0, len(params.Calls))
    passed, failed := 0, 0
    for i, c := range params.Calls {
        result, err := dispatch(ctx, c.Tool, c.Args)
        rec := buildTestRecord(i+1, c, result, err)
        records = append(records, rec)
        if rec.OK {
            passed++
        } else {
            failed++
        }
    }
    summary := ndjsonSummaryRecord{
        Type:        "summary",
        Passed:      passed,
        Failed:      failed,
        Total:       len(records),
        PlanCount:   len(params.Calls),
        Valid:       true,
        Diagnostics: []ndjsonSummaryDiagnostic{},
    }
    return formatNDJSON(records, nil, summary, failed > 0), nil
}

// dispatchSubCall is the test seam; nil in production (falls back to
// p.CallToolV1). Set via *Proxy field assignment in tests.
// (Field declaration: add to Proxy struct in Task 8 if not already.)

func buildTestRecord(n int, c batchCall, result *protocol.ToolCallResultV1, err error) ndjsonTestRecord {
    rec := ndjsonTestRecord{
        Type:        "test",
        N:           n,
        Description: c.Tool,
        Diagnostic:  map[string]any{"tool": c.Tool, "args": json.RawMessage(c.Args)},
        Subtest:     []ndjsonTestRecord{},
        Line:        n,
    }
    if err != nil {
        rec.OK = false
        rec.Diagnostic["error"] = err.Error()
        rec.Diagnostic["kind"] = "transport"
        return rec
    }
    if result != nil && result.IsError {
        rec.OK = false
        rec.Diagnostic["error"] = contentToString(result.Content)
        rec.Diagnostic["kind"] = "tool"
        out := contentToString(result.Content)
        rec.Output = &out
        return rec
    }
    rec.OK = true
    if result != nil {
        out := contentToString(result.Content)
        rec.Output = &out
    }
    return rec
}

func contentToString(blocks []protocol.ContentBlockV1) string {
    var sb bytes.Buffer
    for i, b := range blocks {
        if i > 0 {
            sb.WriteByte('\n')
        }
        if b.Type == "text" {
            sb.WriteString(b.Text)
        } else {
            fmt.Fprintf(&sb, "[%s]", b.Type)
        }
    }
    return sb.String()
}

func emitPreflightBailout(calls []batchCall, rejected []struct {
    index  int
    call   batchCall
    dec    permcheck.Decision
    reason string
}) *protocol.ToolCallResultV1 {
    bail := ndjsonBailoutRecord{
        Type:    "bailout",
        Message: fmt.Sprintf("batch denied: %d of %d sub-calls failed pre-flight", len(rejected), len(calls)),
        Line:    0,
    }
    diags := make([]ndjsonSummaryDiagnostic, 0, len(rejected))
    for _, r := range rejected {
        diags = append(diags, ndjsonSummaryDiagnostic{
            Line:     r.index + 1,
            Severity: "error",
            Message:  fmt.Sprintf("%s: %s (%s)", r.call.Tool, r.reason, r.dec),
        })
    }
    summary := ndjsonSummaryRecord{
        Type:        "summary",
        Skipped:     len(calls),
        Total:       len(calls),
        PlanCount:   len(calls),
        Bailed:      true,
        Valid:       false,
        Diagnostics: diags,
    }
    return formatNDJSON(nil, &bail, summary, true)
}

func formatNDJSON(
    records []ndjsonTestRecord,
    bailout *ndjsonBailoutRecord,
    summary ndjsonSummaryRecord,
    isError bool,
) *protocol.ToolCallResultV1 {
    var buf bytes.Buffer
    enc := json.NewEncoder(&buf)
    if bailout != nil {
        _ = enc.Encode(bailout)
    }
    for _, r := range records {
        _ = enc.Encode(r)
    }
    _ = enc.Encode(summary)
    return &protocol.ToolCallResultV1{
        IsError: isError,
        Content: []protocol.ContentBlockV1{{Type: "text", Text: buf.String()}},
    }
}
```

**Step 3: Add the `dispatchSubCall` field and `NewResolverWithPerms` helper**

In `internal/proxy/proxy.go`, add to the `Proxy` struct:

```go
dispatchSubCall subCallDispatcher // test seam; nil → use CallToolV1
```

In `internal/permcheck/permcheck.go`, add an exported test helper:

```go
// NewResolverWithPerms constructs a resolver from a pre-built perms
// map. Intended for tests that need to inject decisions without a
// MOXIN_PATH walk.
func NewResolverWithPerms(perms map[string]ToolPermInfo) *Resolver {
    return &Resolver{perms: perms}
}
```

**Step 4: Replace the rejection slice's anonymous struct in `emitPreflightBailout`**

Go doesn't accept anonymous struct types in function signatures. Move the `rejection` type to package scope:

```go
type batchRejection struct {
    index  int
    call   batchCall
    dec    permcheck.Decision
    reason string
}
```

Update both call sites.

**Step 5: Compile and run the happy-path test**

Use `hamster.go-build` on `./internal/proxy`. Expected: no errors.

Run: `go test ./internal/proxy/... -run TestBatch_AllAllow -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add internal/proxy/batch.go internal/proxy/batch_test.go internal/proxy/proxy.go internal/permcheck/permcheck.go
git commit -m "feat(proxy): implement HandleBatch happy path + preflight"
```

---

### Task 10: Add `on_error` semantics + remaining tests

**Promotion criteria:** Task 11 registers the builtin once these are green.

**Files:**
- Modify: `internal/proxy/batch.go`
- Modify: `internal/proxy/batch_test.go`

**Step 1: Write the failing tests**

Add to `internal/proxy/batch_test.go`:

```go
func TestBatch_OnErrorStop_DefaultsToStop(t *testing.T) {
    failResult := &protocol.ToolCallResultV1{
        IsError: true,
        Content: []protocol.ContentBlockV1{{Type: "text", Text: "boom"}},
    }
    okResult := &protocol.ToolCallResultV1{
        Content: []protocol.ContentBlockV1{{Type: "text", Text: "ok"}},
    }
    var calls []string
    p := newProxyWithResolverAndDispatch(t,
        map[string]permcheck.ToolPermInfo{
            "fake.tool": {Perm: native.PermsAlwaysAllow},
        },
        func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
            calls = append(calls, name)
            if len(calls) == 2 {
                return failResult, nil
            }
            return okResult, nil
        },
    )

    in := []byte(`{"calls": [
        {"tool":"fake.tool","args":{}},
        {"tool":"fake.tool","args":{}},
        {"tool":"fake.tool","args":{}}
    ]}`)

    res, err := p.HandleBatch(context.Background(), in)
    if err != nil {
        t.Fatal(err)
    }
    if !res.IsError {
        t.Fatal("expected IsError=true")
    }
    if len(calls) != 2 {
        t.Fatalf("expected stop after 2 calls; got %d", len(calls))
    }
    body := res.Content[0].Text
    if !strings.Contains(body, `"directive":{"kind":"skip"`) {
        t.Errorf("expected skip directive on remainder; body=%s", body)
    }
    if !strings.Contains(body, `"bailed":true`) {
        t.Errorf("expected bailed=true; body=%s", body)
    }
}

func TestBatch_OnErrorContinue(t *testing.T) {
    failResult := &protocol.ToolCallResultV1{
        IsError: true,
        Content: []protocol.ContentBlockV1{{Type: "text", Text: "boom"}},
    }
    okResult := &protocol.ToolCallResultV1{
        Content: []protocol.ContentBlockV1{{Type: "text", Text: "ok"}},
    }
    var calls []string
    p := newProxyWithResolverAndDispatch(t,
        map[string]permcheck.ToolPermInfo{
            "fake.tool": {Perm: native.PermsAlwaysAllow},
        },
        func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
            calls = append(calls, name)
            if len(calls) == 2 {
                return failResult, nil
            }
            return okResult, nil
        },
    )

    in := []byte(`{
        "on_error": "continue",
        "calls": [
            {"tool":"fake.tool","args":{}},
            {"tool":"fake.tool","args":{}},
            {"tool":"fake.tool","args":{}}
        ]
    }`)
    res, _ := p.HandleBatch(context.Background(), in)
    if len(calls) != 3 {
        t.Fatalf("expected all 3 calls to run; got %d", len(calls))
    }
    body := res.Content[0].Text
    if !strings.Contains(body, `"passed":2`) || !strings.Contains(body, `"failed":1`) {
        t.Errorf("expected passed=2 failed=1; body=%s", body)
    }
    if !strings.Contains(body, `"bailed":false`) {
        t.Errorf("expected bailed=false; body=%s", body)
    }
}

func TestBatch_PreflightDeny(t *testing.T) {
    p := newProxyWithResolverAndDispatch(t,
        map[string]permcheck.ToolPermInfo{
            // No tools registered → every sub-call resolves Unknown
        },
        func(ctx context.Context, name string, args json.RawMessage) (*protocol.ToolCallResultV1, error) {
            t.Fatalf("dispatch should not be called on preflight rejection")
            return nil, nil
        },
    )
    in := []byte(`{"calls":[{"tool":"any.tool","args":{}}]}`)
    res, _ := p.HandleBatch(context.Background(), in)
    if !res.IsError {
        t.Fatal("expected IsError=true")
    }
    body := res.Content[0].Text
    if !strings.Contains(body, `"type":"bailout"`) {
        t.Errorf("expected bailout record; body=%s", body)
    }
}

func TestBatch_EmptyCallsRejected(t *testing.T) {
    p := newProxyWithResolverAndDispatch(t, nil, nil)
    res, _ := p.HandleBatch(context.Background(), []byte(`{"calls":[]}`))
    if !res.IsError {
        t.Fatal("expected IsError=true")
    }
}

func TestBatch_InvalidOnError(t *testing.T) {
    p := newProxyWithResolverAndDispatch(t, nil, nil)
    res, _ := p.HandleBatch(context.Background(), []byte(`{"on_error":"lol","calls":[{"tool":"x.y","args":{}}]}`))
    if !res.IsError {
        t.Fatal("expected IsError=true")
    }
}

func TestBatch_MalformedJSON(t *testing.T) {
    p := newProxyWithResolverAndDispatch(t, nil, nil)
    res, _ := p.HandleBatch(context.Background(), []byte(`{not json`))
    if !res.IsError {
        t.Fatal("expected IsError=true")
    }
}
```

Add `"code.linenisgreat.com/moxy/internal/native"` to test imports.

**Step 2: Extend `HandleBatch` to emit skip directives**

In `internal/proxy/batch.go`, modify the execution loop:

```go
// Execute sub-calls sequentially.
dispatch := p.dispatchSubCall
if dispatch == nil {
    dispatch = p.CallToolV1
}
records := make([]ndjsonTestRecord, 0, len(params.Calls))
passed, failed, skipped := 0, 0, 0
stopped := false

for i, c := range params.Calls {
    if stopped {
        skipReason := fmt.Sprintf("batch aborted: stopped at #%d", i)
        records = append(records, ndjsonTestRecord{
            Type:        "test",
            N:           i + 1,
            Description: c.Tool,
            OK:          false,
            Directive:   &ndjsonDirective{Kind: "skip", Reason: skipReason},
            Diagnostic:  map[string]any{"tool": c.Tool},
            Subtest:     []ndjsonTestRecord{},
            Line:        i + 1,
        })
        skipped++
        continue
    }
    result, err := dispatch(ctx, c.Tool, c.Args)
    rec := buildTestRecord(i+1, c, result, err)
    records = append(records, rec)
    if rec.OK {
        passed++
    } else {
        failed++
        if onError == "stop" {
            stopped = true
        }
    }
}

summary := ndjsonSummaryRecord{
    Type:        "summary",
    Passed:      passed,
    Failed:      failed,
    Skipped:     skipped,
    Total:       len(records),
    PlanCount:   len(params.Calls),
    Bailed:      stopped,
    Valid:       true,
    Diagnostics: []ndjsonSummaryDiagnostic{},
}
return formatNDJSON(records, nil, summary, failed > 0 || stopped), nil
```

**Step 3: Run all batch tests**

Run: `MOXIN_PATH="" go test ./internal/proxy/... -run TestBatch -v`
Expected: all PASS.

**Step 4: Coverage check**

Run: `just cover-go ./internal/proxy/...`
Expected: `batch.go` functions ≥85%.

**Step 5: Commit**

```bash
git add internal/proxy/batch.go internal/proxy/batch_test.go
git commit -m "feat(proxy): implement batch on_error stop/continue + bailout shaping"
```

---

### Task 11: Register `batch` in the builtin registry

**Promotion criteria:** End-to-end bats test in Task 12 exercises this registration.

**Files:**
- Modify: `cmd/moxy/main.go` (~line 491, just before `p.SetBuiltinTools`)

**Step 1: Register `batch`**

Right before `p.SetBuiltinTools(builtinRegistry)`, add:

```go
builtinRegistry.Register(
    protocol.ToolV1{
        Name:        "batch",
        Description: "Run a sequence of moxin sub-calls under a single permission prompt. Each sub-call must resolve to allow or ask via moxy's perms-request system; deny or unknown aborts the batch. Output is TAP-NDJSON. See moxy-batch(7).",
        InputSchema: json.RawMessage(`{
            "type":"object",
            "required":["calls"],
            "properties":{
                "calls":{
                    "type":"array",
                    "minItems":1,
                    "items":{
                        "type":"object",
                        "required":["tool"],
                        "properties":{
                            "tool":{"type":"string","description":"Namespaced tool name (e.g. grit.tag)"},
                            "args":{"type":"object","description":"Sub-call arguments"}
                        }
                    }
                },
                "on_error":{"type":"string","enum":["stop","continue"],"default":"stop"}
            }
        }`),
        Annotations: &protocol.ToolAnnotations{
            ReadOnlyHint:    boolPtr(false),
            DestructiveHint: boolPtr(true),
        },
    },
    p.HandleBatch,
)
```

**Step 2: Compile**

Use `hamster.go-build` on `./...`. Expected: no errors.

**Step 3: Verify `batch` appears in `tools/list`**

Run: `just tools-list 2>/dev/null | jq -r '.tools[] | .name' | grep -E '^batch$'`

If the recipe is different, find it via `just --list | grep tools`. Expected: `batch` appears in the output.

**Step 4: Commit**

```bash
git add cmd/moxy/main.go
git commit -m "feat(moxy): register `batch` meta-tool in builtin registry"
```

---

## Phase 4 — Integration tests

### Task 12: Add `batch.bats` integration tests

**Promotion criteria:** Phase 5 (coverage baseline) runs against this.

**Files:**
- Create: `zz-tests_bats/batch.bats`
- Possibly modify: `zz-tests_bats/test-fixtures/` if a new fixture moxin is needed

**Step 1: Look at the existing bats common helpers**

Read: `zz-tests_bats/common.bash` and one existing builtin-tool bats test (e.g. anything that exercises `restart`). Mimic their `run_moxy_mcp` shape.

**Step 2: Write the bats tests**

`zz-tests_bats/batch.bats`:

```bats
#!/usr/bin/env bats
# bats file_tags=batch,builtin

load common

setup() {
    common_setup
}

teardown() {
    common_teardown
}

@test "batch happy path: two folio.glob calls" {
    # folio.glob is always-allow, so both sub-calls should resolve to Allow.
    run run_moxy_mcp tools/call '{
        "name": "batch",
        "arguments": {
            "calls": [
                {"tool": "folio.glob", "args": {"pattern": "*.md"}},
                {"tool": "folio.glob", "args": {"pattern": "*.toml"}}
            ]
        }
    }'
    assert_success
    # Output is a single text block containing NDJSON.
    text=$(echo "$output" | jq -r '.content[0].text')
    # Expect two test records and one summary.
    count_tests=$(echo "$text" | jq -c 'select(.type == "test")' | wc -l | tr -d ' ')
    [ "$count_tests" -eq 2 ]
    summary=$(echo "$text" | jq -c 'select(.type == "summary")')
    [ -n "$summary" ]
    passed=$(echo "$summary" | jq -r '.passed')
    [ "$passed" -eq 2 ]
}

@test "batch preflight deny: non-moxin tool aborts" {
    # `restart` is a builtin, has no moxin perm-request → Unknown → bailout.
    run run_moxy_mcp tools/call '{
        "name": "batch",
        "arguments": {
            "calls": [
                {"tool": "restart", "args": {}}
            ]
        }
    }'
    text=$(echo "$output" | jq -r '.content[0].text')
    bailout=$(echo "$text" | jq -c 'select(.type == "bailout")')
    [ -n "$bailout" ]
    # Result must be marked as error
    is_error=$(echo "$output" | jq -r '.isError // false')
    [ "$is_error" = "true" ]
}

@test "batch rejects empty calls array" {
    run run_moxy_mcp tools/call '{"name":"batch","arguments":{"calls":[]}}'
    is_error=$(echo "$output" | jq -r '.isError // false')
    [ "$is_error" = "true" ]
}
```

**Step 3: Verify the new lane is auto-discovered**

The `mkBatsLane` machinery (`flake.nix:629-652`) walks `# bats file_tags=` directives. Our directive is `batch,builtin`. Confirm by listing the discovered lanes — the flake-eval that produces `bats-batch` and `bats-builtin` outputs happens at build time, so the simplest check is to run the bats test directly in devshell.

Run: `just test-bats-dev batch.bats`
Expected: PASS.

**Step 4: Commit**

```bash
git add zz-tests_bats/batch.bats
git commit -m "test(bats): add batch integration tests"
```

---

## Phase 5 — Documentation

### Task 13: Update `CLAUDE.md` with batch tool overview

**Promotion criteria:** N/A.

**Files:**
- Modify: `CLAUDE.md` — section between "### Meta Tools" and "### Synthetic Resource Tools"

**Step 1: Add a paragraph to Meta Tools section**

Read the current "### Meta Tools" section. Append:

```markdown
The `batch` meta tool runs a sequence of moxin sub-calls under a single
permission prompt. Each sub-call must resolve to `allow` or `ask` via
moxy's `perms-request` machinery (in `internal/permcheck`); `deny` or
unknown (non-moxin tools, including builtins and child MCP servers)
aborts the batch with a bailout. Output is TAP-NDJSON mirroring the
`amarbel-llc/tap` `pkgs/ndjson` schema. Sequential execution only;
`on_error` controls whether failure stops or continues. See
`docs/plans/2026-05-20-batch-tool.md`.
```

**Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(claude): document batch meta tool"
```

---

### Task 14: Add `moxy-batch(7)` manpage stub

**Promotion criteria:** Drop this task if no other meta-tool manpages exist; check via `ls cmd/moxy/moxy-*.7` first.

**Files:**
- Create: `cmd/moxy/moxy-batch.7` (mdoc shape mirroring `cmd/moxy/moxy-restart.7`)

**Step 1: Read the restart manpage for shape**

Read: `cmd/moxy/moxy-restart.7`. Mirror its sections (NAME, SYNOPSIS, DESCRIPTION, OPTIONS, EXAMPLES, SEE ALSO).

**Step 2: Write `cmd/moxy/moxy-batch.7`**

Compose with:
- NAME: `moxy-batch — run a sequence of moxin sub-calls under one approval`
- SYNOPSIS: JSON schema highlight
- DESCRIPTION: link to design doc, summarize the perm model
- EXAMPLES: the 25-tag-delete case from issue #258

**Step 3: Verify it builds**

Most moxy manpages are installed via flake `postInstall` substitutions. Confirm the new file is wired up in `flake.nix` if needed (search for `moxy-restart.7` references).

**Step 4: Commit**

```bash
git add cmd/moxy/moxy-batch.7 flake.nix
git commit -m "docs(man): add moxy-batch(7) manpage"
```

---

## Phase 6 — Optional bats coverage lane

### Task 15 (optional): Add `bats-default-cover` flake output

**Promotion criteria:** Not promoted to CI in this change; revisit in a follow-up plan.

**Files:**
- Modify: `flake.nix`

**Step 1: Add the cover lane**

In `flake.nix`, near the `batsLaneOutputs` block (~line 654), add:

```nix
bats-default-cover = pkgs.buildGoCover {
  base = combined;
  coverIntegrationCommand = ''
    ${mkBatsLane { }}/bin/run-bats 2>&1 | tee $out/bats.log || true
  '';
  pnameSuffix = "-bats-cover";
};
```

(Exact `coverIntegrationCommand` shape may need tweaking — `mkBatsLane`'s output is a derivation, not a runnable binary. Check what `batsLane` actually exposes by inspecting its `out` shape. Worst case: the integration command runs `bats` directly against `$out/bin/moxy` after staging the source tree.)

**Step 2: Build the cover lane**

Run: `nix build .#bats-default-cover -L`
Expected: builds without error; `result/coverage.out` exists.

**Step 3: Run the report**

Run: `go tool cover -func=result/coverage.out | grep -E 'internal/(hook|permcheck)/'`
Expected: combined coverage of `hook`+`permcheck` ≥ Phase 1 baseline.

**Step 4: Commit (or discard)**

If the lane proves useful: commit it. If it's a one-shot baseline tool: discard the change after capturing the numbers in this plan.

```bash
git add flake.nix
git commit -m "build(flake): add bats-default-cover lane (one-shot baseline)"
```

---

## Definition of done

- `tools/list` reports `batch`
- `internal/permcheck` ≥85% coverage
- `internal/proxy` batch tests ≥85% coverage
- `internal/hook` coverage ≥ pre-refactor baseline (43.4%) once the resolver moves out
- `just test-bats-tag batch` (or `bats-batch` flake lane) passes
- Design doc is referenced from `CLAUDE.md`
- Issue #258 will close on merge — add `Closes #258` to the merge commit (not per-task commits)
