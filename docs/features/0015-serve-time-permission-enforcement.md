---
status: experimental
date: 2026-10-20
issue: moxy#486
related: moxy#485, moxy#484, FDR 0014 (pkg/moxy), FDR 0011 (dispatch-time
  consent), FDR 0013 (narrowing); clown FDR 0021 §3.3
promotion-criteria: >
  experimental → testing when a clown headless worker (FDR 0021) runs its
  moxy under `permission-mode = "enforce"` pinned through
  MOXY_PARENT_MOXYFILE, or embeds pkg/moxy with Permissions enforcing, and a
  model that calls an each-use tool, a dynamic-deny tool and an unannotated
  tool gets three "moxy denied" results and the run terminates in bounded
  time (FDR 0021 §3.1's property), with no predicate having been asked twice
  where no hook is in front.
---

# Serve-time permission enforcement (`permission-mode = "enforce"`)

## Problem Statement

moxy's permission tiers (`perms-request`: always-allow, each-use, dynamic,
delegate-to-client) are enforced only by the Claude Code PreToolUse and
PermissionRequest hooks, which run `moxy hook` in a separate process before
a call reaches the server. The server's own funnel, `Proxy.callToolV1`,
checks `--expose`, the exclude set and `allow-tools`, then dispatches; it
never consults the resolver (moxy#485; the bats test bb446c3 encoded it). So
any client without the hooks — clown's headless agent loop (FDR 0021), any
non-Claude MCP client — ran each-use and dynamic tools with no decision at
all, including a dynamic predicate whose verdict was deny. The loop's design
needs a server-side verdict it can rely on, and it needs the same verdict
whether it reaches moxy over MCP or embeds it (FDR 0014).

## Interface

### The setting

`permission-mode` is a top-level moxyfile key with two values:

- `delegate` (default, and the historical behaviour): the funnel resolves
  nothing; the hook in front of moxy decides; a client without one runs
  every tool it can name.
- `enforce`: the funnel resolves the tool's tier itself, right after the
  `allow-tools` gate and before dispatch.

It reaches the funnel three ways, all ending in one `perms.Policy`:

| From | How |
| --- | --- |
| the moxyfile | `permission-mode = "enforce"`, merged enforce-wins across the hierarchy |
| the binary | `moxy serve-http --permissions enforce`; it tightens the moxyfile's value, never loosens it (`serve mcp` takes no flags, so the moxyfile is its way) |
| an embedder | `moxy.Options{Permissions: perms.Policy{Mode: perms.Enforce}}`, and per call `Caller.Policy`, which may tighten the instance policy and is refused as an error if it would loosen it |

Narrowing (FDR 0013) locks it: under a parent that enforces, a child that
does not is a violation (`permission-mode: parent enforces, child does not`),
so a spawner that sets enforce on the reference it hands a child cannot be
undone by the child's own moxyfile or by a `restart` reload.

### The mapping

| Resolver outcome | Under `enforce` |
| --- | --- |
| allow (`always-allow`, or a `dynamic` predicate exiting 0) | runs |
| ask (`each-use`; a `dynamic` predicate exiting 1, crashing, timing out or exiting with an unmapped code) | refused |
| deny (a `dynamic` predicate exiting 2) | refused |
| unknown (no `perms-request`, `delegate-to-client`, every tool of a `[[servers]]` child) | refused, unless `enforce-allow` grants it |

The refusal is an `isError` tool result, not a JSON-RPC error, in the shape
of the hook's dontAsk denial (#484): it begins `moxy denied <tool> with
arguments <args>:` and then says why nobody can answer — the check needs a
human and this server enforces permissions itself; the check denied it; the
tool declares no tier. The fixed prefix is the marker a client (clown FDR
0021 §3.4) uses to tell a permission refusal from an ordinary tool error;
`pkg/moxy`'s `Result.Decision` carries the resolved decision structurally.

### `enforce-allow`

The cost moxy#486 named: every child-server tool and every unannotated moxin
tool is refused under `enforce`, because an unannotated tool has not been
reviewed for a world with no human. `enforce-allow = ["smith", "grit.status"]`
grants named tools anyway, with the `allow-tools` grammar (bare name = whole
server or moxin, dotted pair = one tool). It lifts only the unknown outcome;
a declared each-use or a dynamic ask/deny is never overridden by a grant.
Layers add their grants together; narrowing requires each child grant to be
covered by a parent grant. Inert under `delegate`.

### moxy's own tools

Under `enforce`, following what the hook has always done with them: `status`,
`async-result` and `async-cancel` (they read or cancel this session's own
handles) run; `async` and `batch` run, since every call they carry is judged
by this same funnel when dispatched; `restart` (churns children, has always
prompted) is refused.

### Async and batch

- **Preflights.** `batch` admits allow and ask under delegate (its one hook
  prompt covers the list); under enforce only allow, or a grant lifting
  unknown, proceeds. `async` admits ask and unknown under delegate (FDR 0011,
  trusting the hook's dispatch-time consent); under enforce there is no hook
  to have consented, so only allow proceeds.
- **Consent travels with the job.** A detached async job re-enters the funnel
  with the dispatching call's working directory and per-call policy
  override re-attached, so it is judged exactly as the attached call was.
  Since under enforce only allow is ever dispatched, the detached re-check
  cannot disagree with the preflight.

## Examples

A spawner's moxyfile for a headless worker:

```toml
builtin-native = false
permission-mode = "enforce"
allow-tools = ["folio", "rg", "man", "smith.issue-get"]
enforce-allow = ["smith.issue-get"]   # a child server's tool: no tier, granted explicitly
```

A plain `tools/call` of an each-use tool under it:

```json
{"isError":true,"content":[{"type":"text","text":"moxy denied permfix.asky with arguments {}: its permission check needs a human to approve this call (each-use: requires explicit approval), and this server enforces permissions itself, so nobody can answer. Retry with arguments the check allows (for example a path inside the working tree), or ask the operator to run it."}]}
```

The same from an embedder:

```go
inst, _ := moxy.Open(ctx, moxy.Options{Config: cfg, Permissions: perms.Policy{Mode: perms.Enforce}})
res, _ := inst.Call(ctx, moxy.Call{Name: "folio.read", Args: args, Caller: moxy.Caller{Cwd: worktree}})
// res.Decision == perms.Allow and the content, or res.IsError with res.Decision == perms.Ask / Deny / Unknown
```

## Limitations

- **The server is only as good as the resolver's inputs.** Tiers are read
  from the moxin directories moxy serves; `MOXIN_PATH` decides them, and
  FDR 0013's `resolved-moxins` record is what pins that for a child.
- **Checked twice with a hook in front.** A Claude Code session whose moxy
  enforces runs each dynamic predicate in the hook and again in the server.
  That is the price of not assuming the hook exists; keep `delegate` for
  hook-fronted sessions, which is why it stays the default.
- **`enforce-allow` is coarse.** It grants a tool or a server, not an argument
  pattern; argument-sensitive rules for unannotated tools belong in a
  `dynamic` predicate on the moxin, or in the client's own policy (FDR 0021
  §3.4(b)).
- **Resources and prompts are not gated** by the policy, as `allow-tools`
  does not gate them (FDR 0013 Limitations). A `resource-read` synthetic
  tool is a child tool and is gated.
- **Not a sandbox.** An enforcing moxy stops a model from calling what the
  tiers refuse through moxy; it does not stop a process that can run the
  moxin binaries directly. The unit is the boundary (clown FDR 0021 §3.3).
- **`restart` under enforce is refused outright** rather than prompting;
  an embedder restarts through `Instance.Restart` / `Reload`, the operator
  through a new process.

## Tuning Levers

| Lever | Current | Rationale | Change signal |
|---|---|---|---|
| unknown under enforce | refused unless granted | an unannotated tool has not been reviewed for unattended use (FDR 0021 §3.3) | operators grant whole servers routinely in every brief |
| meta tools under enforce | status/async-result/async-cancel/async/batch allow, restart ask | mirrors the hook's builtinAutoAllow and inner-tool treatment | a headless brief needs restart; then `enforce-allow = ["restart"]` could lift it |
| refusal shape | "moxy denied …" text prefix + `Result.Decision` | the hook's dontAsk denial already trained clients on the prefix | a client wants a structured `_meta` marker on the wire |

## More Information

- moxy#486 (this record), moxy#485 (the gap), moxy#484 (the dontAsk shape
  this mirrors); bats `zz-tests_bats/server_side_perms.bats` (tag
  `server_perms`), formerly skipped, now the acceptance test.
- FDR 0014 (pkg/moxy; `Options.Permissions`, `Caller.Policy`,
  `Result.Decision`), FDR 0011 (the hook-trusting async posture this mode
  does not inherit), FDR 0013 (narrowing; the lock).
- clown FDR 0021 §3.3 ("what the loop needs from enforce mode") and §3.4
  (the permission marker).
- moxyfile(5) TOP-LEVEL KEYS and NARROWING; moxin(7) PERMISSIONS;
  moxy(1) `serve-http --permissions`.
