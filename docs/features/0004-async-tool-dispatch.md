---
status: testing
date: 2026-06-06
promotion-criteria: live smoke passed 2026-06-06 (real dispatches woken via the
  clown channel with succeeded/failed/cancelled states, full results via
  `async-result`, digests resolved from the user-level store by an independent
  process, allow-only and permit-async rejections verified); remaining gate for
  accepted — no tuning-lever adjustment needed for 2 weeks of real use, and
  the #322 cancellation-kill gap resolved or explicitly accepted
---

# Async tool dispatch (meta tools + clown job-wakeups)

## Problem Statement

Every moxy tool call is synchronous: the agent blocks until the subprocess or
child server returns, so a long-running tool (a slow API sweep, a big `rg`
over a huge tree, a multi-step batch) occupies the agent's turn for its full
duration. The clown job-wakeup channel (RFC-0009, proven by
`get-hubbed.ci-watch`) already lets background work wake the agent on
completion — but nothing lets the agent *put a moxy tool call into the
background* in the first place. Any moxy tool, individually or as a batch,
should be dispatchable asynchronously: return a handle immediately, run
detached, and wake the agent when the result is ready.

## Interface

Three new meta tools (joining `restart` and `batch`), plus an `async` flag
on `batch`.

### `async` — dispatch one tool call in the background

Input: `{tool: "<server>.<tool>", args: {...}, timeout?: "<duration>"}` — the
`tool`/`args` shapes `batch` sub-calls use, plus an optional `timeout`
duration string (`"10m"`, `"90s"`). When the job exceeds its timeout (the
per-call value, else the 30-minute server default) moxy kills the whole
process tree and terminalizes it with status `timeout` (§State mapping). An
unparseable or non-positive `timeout` is rejected synchronously.

1. **Eligibility pre-resolution** (same machinery as `batch`): the call is
   resolved through moxy's `perms-request` system *before* anything runs.
   Eligibility = **permission resolves to allow AND the tool does not
   declare `permit-async = false`** (#317). `deny`, `ask`, and unknown
   tools are rejected synchronously with a structured error — there is no
   client to prompt once the call is detached. The `permit-async` boolean
   (top-level schema-3 TOML key; omitted = eligible) is the author's brake
   for tools that are allow-safe but should not run detached (interactive,
   ordering-sensitive, trivially fast); its rejection text is distinct from
   the permission rejection. The key is deliberately shaped to map onto MCP
   `execution.taskSupport` (`forbidden`/`optional`) but is NOT yet emitted
   on `tools/list` — surfacing it before moxy wires a `TaskProvider` could
   bait tasks-aware clients into task-augmented calls moxy can't serve.
2. **Job open**: moxy runs `${RINGMASTER_BIN:-ringmaster} start --source moxy
   --label <tool>` and adopts the printed job id (e.g. `rg.search-3f2a8b1c`
   — ringmaster's label sanitizer keeps dots; the job-id charset is
   `[A-Za-z0-9._-]`) as the async handle. (clown RFC-0015 promoted the job
   verbs off `clown job <verb>` onto the standalone `ringmaster` binary, on
   PATH wherever clown is installed; the swap is behavior-preserving, and
   `RINGMASTER_BIN` is an optional override for tests/pinning.) Implementers
   MUST NOT assume an id always comes
   back: with `CLOWN_DISABLE_JOB_WAKEUP=1` the `ringmaster` commands are
   exit-0 no-ops that print **nothing** — empty stdout on a zero exit is the
   normal disabled-channel signature, not an error. In that case (and when the
   call fails outright — e.g. `ringmaster` absent from PATH), moxy mints a
   local id of the same shape — async still works, the agent just polls
   `async-result` instead of being woken.
3. **Detached dispatch**: the call runs through the normal `CallToolV1`
   dispatch (statsd metrics included) on a context detached from the
   requesting MCP call, governed by the same ~16-slot concurrency cap as the
   rest of dispatch.
4. **Immediate return**: `{job_id, tool, status: "running"}`.
5. **Terminal**: the full result is written to the madder store, the
   in-memory index is updated, and moxy emits
   `ringmaster done <job_id> --state <state> --message "<tool>:
   <first-line summary> (madder <digest>)" --result-ref
   "madder://blobs/<digest>"`. The `result_ref` is the **machine-readable
   artifact URI** (a terminal that produced no result carries none), so a
   journal reader — this or another moxy process — recovers the result from
   the done record alone; the digest also rides in the message for the human
   wake line. The state is NOT repeated in the message text — the wake line
   already renders it from the record's state field.

State mapping: clean result → `succeeded`; dispatch error or `isError`
result → `failed`; `async-cancel` → `cancelled`; a deadline (per-call or
default max-runtime) → `timeout`; moxy graceful shutdown → `interrupted`.
`timeout` is a **moxy-only** status that `async-result` reports verbatim; on
clown's wire it is emitted as `interrupted` (clown accepts only
succeeded/failed/cancelled/interrupted, and a deadline IS an interruption).
Every spawned job reaches a terminal `done` — on shutdown moxy sweeps
in-flight jobs and emits `interrupted` for each (a hard crash is the accepted
producer-death gap; the job sits open until clown's 7-day journal GC).

Both the deadline and `async-cancel` rely on a process-group kill at the
native exec layer (`Setpgid` + a group SIGTERM on ctx-cancel + `WaitDelay`):
without it a deeper process or a SIGTERM-ignoring leaf keeps the inherited
pipes open and `cmd.Wait()` blocks forever, leaving the job wedged `running`
with no terminal wakeup (#344/#345). `async-cancel` returns a `detail` field
while a still-dying tree exits so a transient `running` isn't read as
failure-to-act.

### `async-result` — fetch a stored result

Input: `{job_id}`. **Journal-first**: when clown's journal is reachable it is
the authority for state and the result reference — `async-result` reads
`ringmaster read --job <id> --json`, takes the last terminal record's
`result_ref`, and fetches the `ToolCallResultV1` from the `moxy-async` store.
This resolves jobs launched by **another** moxy process or session (#321), not
just this process's own. When the journal is unavailable (channel disabled,
clown absent, or a locally-minted id) it falls back to the in-memory index,
which also owns the moxy-only `timeout` distinction (a journal read of a
timed-out job sees the wire state `interrupted`, upgraded back to `timeout`
only for this process's own jobs). Running jobs return `{status: "running",
...}` merged with the live `ringmaster status` probe (elapsed/last_activity/
spool_bytes/tail, FDR-0005) so the tool doubles as a poll surface when wakeups
are disabled. Unknown ids return a structured error listing live job ids.

### `async-cancel` — cancel a running job

Input: `{job_id}`. Context-cancels the in-flight dispatch; the job reaches
terminal `cancelled` and wakes the agent like any other terminal state.
Cancelling an already-terminal job is a no-op that returns the terminal
state.

### `batch {async: true, ...}` — background a whole batch

`batch` gains an optional `async` flag. The existing preflight (all sub-call
perms resolved to allow, else single bailout) runs synchronously; on success
the batch backgrounds as **one** job (`--label batch`), and the stored
result is the batch's normal TAP-NDJSON document. Per-sub-call jobs were
considered and rejected: one wake per batch matches how agents consume batch
results, and avoids flooding the notification channel.

## Result store

Results are written to a **user-level madder store** named `moxy-async`.
Unprefixed madder store ids resolve to `$XDG_DATA_HOME/madder/blob_stores/`
(see madder(1) FILES), so the store is shared across sessions and worktrees —
unlike the CWD-relative `.default` store moxy uses for inline-result
substitution, which is scoped per worktree and dies with it.

**Provisioning: the store is declared by home-manager; moxy only writes,
never creates.** Rationale: madder's `init` currently disagrees with `write`
about unprefixed store-id scope (`init` lands in the nearest ancestor
`.madder`, shadowing XDG — madder#227), so creating the store from inside a
worktree is unreliable; and a user-level resource shared by every session is
infrastructure, which the eng convention provisions declaratively, not on
first use by whichever process gets there first. Until the store exists,
async degrades gracefully: jobs still reach terminal states and `async-
result` serves from the in-memory index — only the digest in the wake
message is missing (the write failure is logged to lifecycle.log).

Rejected alternatives: worktree `.default` (covers in-session fetch and
moxy-restart, but results die with `sc close` — the user requires
session-surviving results); moxy-creates-XDG-store (blocked on madder#227
and the wrong ownership model regardless).

The job index (`job_id → {tool, state, digest, summary, started, finished,
cancel}`) is **in-memory**, and is now the *fallback* read authority rather
than the primary. The authorities split three ways: clown's journal is the
**state** authority on the read path (`async-result` reads it first), the
`moxy-async` store is the **result** authority, and the in-memory index serves
degraded mode (no journal) and holds the OS process handle for the kill. It
must NOT be removed — it is the only source of truth when the journal is
unavailable. Retention is likewise three-domained:

- clown's journal records bound the *state* read path. Note clown#126
  (cleanup-on-terminate): acked terminal records get a 24h resting-retention
  and are then reaped, so a long-finished cross-session job may no longer be in
  the journal;
- the madder blobs persist independently until explicitly reaped, so a job
  whose journal record was reaped can still have a live result; the wake
  message also embeds the digest, so even after a moxy restart loses the index
  the agent can `madder cat <digest>` straight from the notification line.

## Examples

Background a slow search, keep working, get woken:

    async {tool: "rg.search", args: {pattern: "TODO", path: "/big/tree"}}
    → {"job_id": "rg.search-3f2a8b1c", "tool": "rg.search", "status": "running"}

    ... agent does other work ...

    [clown-job] moxy rg.search-3f2a8b1c succeeded: rg.search:
    412 matches (madder blake2b256-...) · madder://blobs/blake2b256-...

    async-result {job_id: "rg.search-3f2a8b1c"}
    → full ToolCallResultV1

Background a destructive batch after one permission prompt:

    batch {async: true, calls: [{tool: "grit.tag", args: {...}}, ...]}
    → {"job_id": "batch-9c01d4e2", "status": "running"}
    ... wake on completion; async-result returns the TAP-NDJSON document

Cancel:

    async-cancel {job_id: "rg.search-3f2a8b1c"}
    → {"job_id": "rg.search-3f2a8b1c", "status": "cancelled"}

## MCP-native tasks (forward compatibility)

MCP 2025-11-25 defines task-augmented tool invocation, and the pinned go-mcp
already ships the full surface (`server.Options.Tasks` / `TaskProvider`
{GetTask, GetTaskResult, CancelTask, ListTasks}, `tasks/get|result|cancel|
list`, `ToolExecution.TaskSupport`). The v1 meta-tool design deliberately
shapes its bookkeeping to back that interface later: the job index IS a
`TaskProvider` store (`GetTask` ≈ index lookup, `GetTaskResult` ≈
`async-result`, `CancelTask` ≈ `async-cancel`, `ListTasks` ≈ index scan).
Today no known client (including Claude Code) sends task-augmented calls, so
wiring `Options.Tasks` is future work gated on demonstrated client support —
not part of v1.

## Limitations

- **Ask-gated tools cannot run async.** Only calls whose permission resolves
  to allow may background. This is a safety posture, not a gap to fill.
- **Producer-death gap.** If moxy crashes (not graceful shutdown), in-flight
  jobs never emit a terminal `done`; they sit open until clown's 7-day
  journal GC. Accepted for v1, same posture as spinclass's interrupted case.
- **Index is process-local, and deliberately retained as the degraded
  fallback.** A moxy restart forgets job ids; cross-process state now comes
  from clown's journal (the read authority), and results remain reachable via
  the digest in the wake message. The index is NOT dead code — it is the sole
  source of truth when the journal is unavailable (disabled channel,
  locally-minted id), so "finishing the migration" by deleting it would break
  standalone moxy.
- **Cross-session reads collapse `timeout` to `interrupted`.** clown's wire
  vocabulary has only four states (RFC-0009 §5), so a timed-out job reads back
  as `interrupted` to any process other than the one that launched it; that
  process upgrades it to `timeout` from its local index, and the human summary
  ("timed out after <d>") carries the nuance regardless. A machine-readable
  cross-session signal would be a future RFC-0009 addition (a fifth state or a
  sub-reason field), not a `result_ref`/message hack.
- **Terminal-record reaping (clown#126).** Once clown GCs a job's terminal
  record (24h after ack), `async-result` falls back to this process's index;
  if that's also gone (restart), a >24h-old cross-session result is
  unrecoverable via `async-result` — the agent had the digest in the original
  wake line.
- **No progress events.** `started`/`progress` records are journal-only in
  the clown channel and v1 emits none; only terminal states wake the agent.
- **Sequential batch semantics unchanged.** `async: true` changes *when* the
  batch runs, not how — sub-calls still execute sequentially per the batch
  contract.
- **Cancel/timeout may orphan a SIGTERM-ignoring leaf (#344/#345).** The
  process-group kill SIGTERMs the whole tree and `WaitDelay` force-closes the
  pipes so the dispatch always terminalizes, but a leaf that ignores SIGTERM
  and survives the grace is left orphaned (reparented to init) rather than
  SIGKILLed as a group. The job still reaches its terminal state on time; the
  stronger group-SIGKILL-on-escalation is a possible follow-up.
- **Wake summaries of threshold-cached results show the truncation banner
  (#323).** Cosmetic; `firstLine` should skip banner lines.
- **Store reaping is manual.** Nothing auto-reaps `moxy-async` blobs in v1;
  content-addressed storage makes this cheap to defer.
- **The result store must be provisioned out-of-band** (home-manager).
  Until it exists, wake messages carry no digest and results are only
  reachable from the dispatching moxy process's in-memory index.

## Tuning Levers

| Lever | Current | Rationale | Change signal |
|---|---|---|---|
| max job runtime | 30 min default; per-call `timeout` duration-string override | bounds the terminal-done guarantee without strangling real long jobs (ci-watch uses 6 h for CI) | legitimate jobs hitting the ceiling |
| kill grace (`killGrace`) | 10 s | time between the group SIGTERM and Go force-closing the pipes + SIGKILLing the child; long enough for graceful exit, short enough to bound the wedge | jobs taking >10 s to flush on cancel, or wedges feeling slow to terminalize |
| concurrent async jobs | shared ~16-slot dispatch cap | clown etiquette is "low dozens"; reusing the existing governor avoids a second knob | agents queueing behind the cap on real workloads |
| index entry retention | process lifetime, no cap | sessions are bounded; digest-in-message covers restarts | memory growth or agents needing old ids listed |
| store reaping | none | content-addressed blobs are cheap; reaping needs a policy not a guess | `moxy-async` store size complaints |
| wake message summary | first line of result, truncated | one notification line must stay readable | agents consistently needing more context before fetching |

## More Information

- moxy#314 (feature request), moxy#267 (batch madder-blob formatter — adjacent)
- **Journal-as-state-authority (2026-06-13):** `async-result`/`async-cancel`
  re-backed on clown's journal (this doc's read path) — moxy#321 (cross-session
  visibility), moxy#341 (status/tail parity), moxy#131 (timeout doc fix). The
  full clown-ownership end-state (clown executes the call, agents inspect via
  clown's own tools) is clown#117, gated on clown#112 (result blobs over the
  channel) and clown#134 (job-as-executor) — deliberately out of scope here.
  Heads-up: clown#126 changed terminal-record retention (24h resting + reap).
- clown: RFC-0009 (job-wakeup channel + record schema), RFC-0010 (output spool
  + status probe), RFC-0011 (job-platform MCP tools — deliberately no cancel),
  clown-job(1), FDR-0013 (channel consumers; ci-watch live proof)
- Precedents in this repo: `get-hubbed.ci-watch` (producer contract,
  terminal-done guarantee, kill-switch posture), `batch`
  (docs/plans/2026-05-20-batch-tool.md — permission preflight machinery this
  design reuses)
- Pinned clown contract (2026-06-13, clown/fond-sycamore): source=`moxy`,
  label=tool name or `batch`, `result_ref` = `madder://blobs/<digest>` (the
  machine-readable artifact URI), read back via `ringmaster read --job <id>
  --json` (was `clown job read` before the RFC-0015 CLI rename), shell-out
  producer only (no native journal speaker), job-id is the agent's dedupe key
