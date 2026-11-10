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

## Amendment (proposed, 2026-11-10): enforcement under a profile

Status: proposed text, no code. It describes what the funnel does when a
blessed capability profile is in effect (cutting-garden FDR 0028 `ajour`,
clown RFC 0018/0019, circus D2). Nothing here runs today: linking
`pkgs/ajour` needs cutting-garden as a flake input (a lock change the operator
has to approve), and shadow mode (moxy#489) only logs the verdict. The rulings
below are the operator's of 2026-11-10 as relayed by the coordinator and by
cutting-garden/swift-plum (not verified by me against a primary record); the
ajour side landed on cutting-garden master `8295848`. The reasoning is in
`docs/plans/2026-11-10-permissions-unification-research.md`.

### Rulings this amendment records

1. **Option B for `ask`.** A profile's `perm=allow` on the matched node
   pre-approves a human-prompt tier for that call. A profile's `perm=ask`
   admits the call subject to a human. A node with no `perm` leaves the
   manifest tier as it is today.
2. **Option A for `dynamic`.** A `dynamic` tool's predicate (`folio-perms` and
   the grit `*-perms` predicates) stays a *floor* under any profile until
   `fs-path-v1` covers it. A predicate deny binds even when the profile says
   allow; a predicate ask yields to an explicit profile `perm=allow`. That
   extends the "both must admit" rule, with one exception: an explicit allow
   pre-approves a human-prompt tier and never overrides a deny.
3. **Kind source.** Derived from annotations and `[resources]` where possible;
   author override only where the hints cannot say (`exec`). Not decided here
   beyond that (see the research note and `docs/plans/2026-11-10-derive-tiers-result.md`).

### The order (closed by default)

1. Shape gates: `--expose`, the exclude set, `allow-tools` (unchanged).
2. The ajour Verdict, when a profile is in effect: absent or Refused refuses;
   Ask is resolved per posture; Admitted continues. moxy does not re-derive
   the mapping: ajour's `Verdict.Resolve(posture, canEscalate)` returns
   `Run | Prompt | Escalate | Refuse`, and moxy and the clown hook both use it.
3. The tier and floor, as a further restriction only: `each-use` and
   `delegate-to-client` are skipped for a call the profile allowed (ruling 1);
   a `dynamic` predicate still runs and a deny from it refuses (ruling 2);
   with no profile in effect the tier decides exactly as in "The mapping"
   above.
4. The policy broker, per resource (unchanged).

`Evaluate` is posture-free. moxy supplies the posture (attended or
unattended, from the session) and whether it has an escalation channel. Under
`Resolve`, an unattended `Ask` is `Escalate` when moxy has a channel and
`Refuse` when it does not, so the refusal text for it is the existing "moxy
denied ..." shape with a reason saying nobody can answer.

### Shadow mode

Shadow mode is unaffected: it evaluates and logs and enforces nothing. The
log's `verdict` field is the Verdict's string, so an `ask` verdict is logged
as such with no format change; it still records the tool, field names and
types, never values (`pkg/shadow`).

### Open items

**Operator decisions (nothing is built until both are made):**

- **Flake inputs.** Linking `pkgs/ajour` (the evaluator, for moxy#489) and
  `pkgs/catalogcontract` (the field kinds, `LockDigest`) means cutting-garden
  and clown become inputs of moxy: lock changes, which the operator has to
  approve. Until then `pkg/shadow` carries a local mirror of the `Call` shape
  and a copy of `catalog.json`.
- **Unconfined posture softness.** `MOXY_POSTURE` and `MOXY_PROFILE_LAYERS` are
  unforgeable only for confined sessions (clown). For an unconfined session, as I
  read it (not verified), an agent with shell access could start its own moxy
  with different values.
  How much that matters, and what an unconfined session is allowed to be
  enforced as, is the operator's call.

**Owners' items, with the recommended shape:**

- **Posture and the escalation channel.** Not defined in moxy yet. The
  recommendation from cutting-garden/swift-plum (2026-11-10; a recommendation
  to the owning lanes, not a ruling): posture is session state the launcher
  knows (spinclass launches a session attended or unattended and clown
  inherits it), so moxy should be *told* it at start, by an environment
  variable or a moxyfile key the launcher writes (for example
  `[permissions] posture = "attended" | "unattended"`), and never infer it or
  read it from the profile head's `mode`, which is a declaration to cross-check
  (a mismatch refuses at launch). The escalation channel belongs to clown (a
  troupe message to the operator or coordinator); moxy has no messaging. So in
  moxy's funnel `canEscalate` is always false: an unattended `Ask` is
  `Refuse` there, and the layer that can escalate is the clown PreToolUse hook,
  which sees the same Verdict through `Resolve` and owns the message. The two
  enforcers must not both try to escalate. Owners: spinclass (posture),
  clown (channel and hook); routed by cutting-garden/swift-plum to
  circus/eager-holly.
- **Handing moxy the profile stack.** Recommendation from the same source: not
  an in-process `Options` field built by moxy. The stack is the signed act
  chain (piggy RFC 0013) that spinclass resolves at launch (FDR 0034); the
  launcher writes the layer documents to disk in stack order and names them to
  moxy (for example `[permissions] profile-layers = [".../0-stencil.espalier",
  ".../1-patch.espalier"]`), each pinned by the chain's digest. moxy parses
  them with ajour's `ParseStencil` and `ParsePatch` and never constructs a
  layer. That keeps moxy decoupled from the chain walk and lets
  `cutting-garden-ajour` replay the exact stack for a refusal. Owners: spinclass
  (writes the layers), moxy (reads them; the moxyfile key is moxy's to define
  once the owners agree). The earlier suggestion here of an `Options` field is
  superseded.
  Spellings (moxy's; agreed 2026-11-10 by spinclass and clown, as relayed by
  cutting-garden/swift-plum): two environment variables read in `cmd/moxy` only and handed to
  the library as options, exactly as `MOXY_PARENT_MOXYFILE` is. Not moxyfile
  keys: a moxyfile hierarchy includes the working directory's own `./moxyfile`,
  which the agent can write, so a policy key there is forgeable; an environment
  variable set by the launcher at spawn is not (clown: unforgeable only for
  confined sessions). `MOXY_POSTURE = attended | unattended`, set by clown from
  its own launch flags. `MOXY_PROFILE_LAYERS = <absolute path>`, the per-session
  layers file spinclass writes, named by clown; its format is spinclass's: ordered entries
  of `{path, role: stencil | patch, digest, act}`, where `role` matches
  `piggy-act-verdict-v1`'s `layers[].role` and `act` is the piggy act digest the
  layer came from (provenance back to the signed chain). spinclass and clown
  both accepted the two variables, and spinclass the entry shape. moxy reads `path`, `role` and `digest`, and
  carries `act` into its refusal and shadow-log text unchanged, never
  interpreting it. Fail closed:
  layers set but posture unset, the file unreadable or unparseable, or a layer
  that does not parse with ajour or whose digest does not match (once the check
  exists), all refuse to start, as an unreadable parent reference does. No
  layers set means no profile: the tier decides as today. The examples of a
  `[permissions]` moxyfile key above are superseded by these.
  Two refinements, both pending their owners. Posture is not read from an
  inherited environment variable (the agent can export one); it is what clown
  sets for moxy from its own launch flags (spinclass/slim-myrtle, 2026-11-10).
  The layer files are written by spinclass from the piggy verdict's `layers[]`
  to a per-session file out of a confined unit's reach, and clown names it to
  moxy. Verifying a layer's digest (agreed 2026-11-10 per cutting-garden/swift-plum,
  relaying piggy/green-larch; relayed, not read by me in RFC 0013): a stencil's
  digest is a blake2b256 markl id over the canonical `Write` bytes, and the
  verifier needs only the layer bytes and the verdict's edge. cutting-garden
  adds `Stencil.Digest()` (and a patch equivalent) to `pkgs/ajour`, so moxy's
  check is: `ParseStencil`, `Digest()`, compare to the edge. moxy does no
  hashing of its own. Until ajour ships it there is no code to write.
  The variables are read from the spawn environment once at start. moxy has no
  way to tell its spawner from an inherited environment, so what it can do is
  narrow: it takes posture and layers from these variables only, never from a
  moxyfile, a tool argument or a reload, and ignores them after start. That the
  variables really came from the launcher is the launcher's guarantee
  (unforgeable only for confined sessions; the unconfined-posture softness is
  an open operator item).
- The 13 `always-allow` tools that are not read-only by their hints would be
  `ask` under a derived default; whether to derive at all is the open kind
  question, not part of this amendment.

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
