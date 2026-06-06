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

# Async tool dispatch (meta tools + clown job wakeups)

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

Input: `{tool: "<server>.<tool>", args: {...}}` — the same shapes `batch`
sub-calls use.

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
2. **Job open**: moxy runs `${CLOWN_BIN:-clown} job start --source moxy
   --label <tool>` and adopts the printed job id (e.g. `rg.search-3f2a8b1c`
   — clown's label sanitizer keeps dots; the job-id charset is
   `[A-Za-z0-9._-]`) as the async handle. Implementers MUST NOT assume an id always comes
   back: with `CLOWN_DISABLE_JOB_WAKEUP=1` the `clown job` commands are
   exit-0 no-ops that print **nothing** — empty stdout on a zero exit is the
   normal disabled-channel signature, not an error. In that case (and when
   `CLOWN_BIN` is unset or the call fails outright), moxy mints a local id
   of the same shape — async still works, the agent just polls
   `async-result` instead of being woken.
3. **Detached dispatch**: the call runs through the normal `CallToolV1`
   dispatch (statsd metrics included) on a context detached from the
   requesting MCP call, governed by the same ~16-slot concurrency cap as the
   rest of dispatch.
4. **Immediate return**: `{job_id, tool, status: "running"}`.
5. **Terminal**: the full result is written to the madder store, the
   in-memory index is updated, and moxy emits
   `clown job done <job_id> --state <state> --message "<tool>:
   <first-line summary> (madder <digest>)" --result-ref "moxy async-result
   <job_id>"`. The state is NOT repeated in the message text — the wake
   line already renders it from the record's state field. The agent's wake
   line carries everything needed to act.

State mapping: clean result → `succeeded`; dispatch error or `isError`
result → `failed`; `async-cancel` → `cancelled`; max-runtime timeout or moxy
graceful shutdown → `interrupted`. Every spawned job reaches a terminal
`done` — on shutdown moxy sweeps in-flight jobs and emits `interrupted` for
each (a hard crash is the accepted producer-death gap; the job sits open
until clown's 7-day journal GC).

### `async-result` — fetch a stored result

Input: `{job_id}`. Returns the job's state plus, when terminal, the full
original `ToolCallResultV1` (from the index entry's madder digest). Running
jobs return `{status: "running", started: ...}` so the tool doubles as a
poll surface when wakeups are disabled. Unknown ids return a structured
error listing live job ids.

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
cancel}`) is **in-memory**. Two retention domains, deliberately decoupled
(store = system of record, journal = wake layer, mirroring the spinclass
chat migration):

- clown's journal GC (7 days) bounds the *wake* records;
- the madder blobs persist until explicitly reaped, and the wake message
  embeds the digest — so even after a moxy restart loses the index, the
  agent can `madder cat <digest>` straight from the notification line.

## Examples

Background a slow search, keep working, get woken:

    async {tool: "rg.search", args: {pattern: "TODO", path: "/big/tree"}}
    → {"job_id": "rg.search-3f2a8b1c", "tool": "rg.search", "status": "running"}

    ... agent does other work ...

    [clown-job] moxy rg.search-3f2a8b1c succeeded: rg.search:
    412 matches (madder blake2b256-...) · moxy async-result rg.search-3f2a8b1c

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
- **Index is process-local.** A moxy restart forgets job ids; results remain
  reachable only via the digest embedded in the wake message. A durable
  index is deliberate future work, not v1.
- **No progress events.** `started`/`progress` records are journal-only in
  the clown channel and v1 emits none; only terminal states wake the agent.
- **Sequential batch semantics unchanged.** `async: true` changes *when* the
  batch runs, not how — sub-calls still execute sequentially per the batch
  contract.
- **Cancellation doesn't kill grandchildren (#322).** `async-cancel` kills
  the native tool's direct child, but its descendants survive and the
  dispatch only unwinds when they release the pipes — the job classifies
  `cancelled` correctly, but the underlying work runs to natural completion.
  Proven live: a cancelled `sleep 300` recipe terminal-ized exactly at
  start+300s. Fix direction: process-group kill and/or `cmd.WaitDelay` at
  the native exec layer.
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
| max job runtime | 30 min default, per-call override | bounds the terminal-done guarantee without strangling real long jobs (ci-watch uses 6 h for CI) | legitimate jobs hitting the ceiling |
| concurrent async jobs | shared ~16-slot dispatch cap | clown etiquette is "low dozens"; reusing the existing governor avoids a second knob | agents queueing behind the cap on real workloads |
| index entry retention | process lifetime, no cap | sessions are bounded; digest-in-message covers restarts | memory growth or agents needing old ids listed |
| store reaping | none | content-addressed blobs are cheap; reaping needs a policy not a guess | `moxy-async` store size complaints |
| wake message summary | first line of result, truncated | one notification line must stay readable | agents consistently needing more context before fetching |

## More Information

- moxy#314 (feature request), moxy#267 (batch madder-blob formatter — adjacent)
- clown: RFC-0009 (job-wakeup channel), clown-job(1), FDR-0013 (channel
  consumers; ci-watch live proof)
- Precedents in this repo: `get-hubbed.ci-watch` (producer contract,
  terminal-done guarantee, kill-switch posture), `batch`
  (docs/plans/2026-05-20-batch-tool.md — permission preflight machinery this
  design reuses)
- Pinned clown contract (2026-06-06, clown/sleek-sumac): source=`moxy`,
  label=tool name or `batch`, result-ref `moxy async-result <job_id>`,
  shell-out producer only (no native journal speaker), job-id is the agent's
  dedupe key
