---
status: experimental
date: 2026-06-08
promotion-criteria: a real multi-minute async job (just-us-agents.run-recipe
  wrapping nix build + bats) probed mid-flight via async-result shows elapsed,
  fresh last-activity, and a recognizable output tail against the installed
  clown (RFC-0010 spool+status shipped on clown master 575657d); the
  agent-side babysitting patterns from #341 (derivation re-eval, side-channel
  file globbing) no longer observed in session transcripts
---

# Async job live status (output spool + clown job status)

## Problem Statement

A running `async` job is a black box: `async-result` returns only
`{job_id, started, status: "running"}` until the terminal wake, so for
multi-minute jobs (nix builds + bats lanes via `just-us-agents.run-recipe`)
the agent cannot tell a working job from a wedged one (#341). Agents resort
to probing side effects — re-evaluating nix derivations (contending the eval
lock) or globbing harness temp files. spinclass solved this privately for its
merge jobs (`session-job-status`: state, elapsed, last-activity, output
tail); rather than moxy growing a second private copy, the probing surface
moves into clown's job channel (RFC-0010), which already owns each job's
lifecycle journal — moxy's part is to *produce* the output spool and to
mirror the probe in its `async-result` poll surface.

## Interface

### Producer: output spool from the native exec layer

When `async` dispatches a job, moxy resolves the job's spool path via
`${CLOWN_BIN:-clown} job spool-path <job_id>` (empty when the channel is
disabled — then no spool is written) and threads an output sink into the
dispatch context. The native moxin exec layer (`runMoxinProcess`) tees the
child's stdout and stderr into the sink as they arrive, interleaved in
arrival order, alongside the existing in-memory buffers. The spool is
best-effort (RFC-0010 §1): write failures are logged to lifecycle.log and
never affect the dispatch. The sink is closed (and never written again) when
the job reaches a terminal state.

Only native moxin dispatches produce output incrementally; a job whose tool
resolves to a child MCP server writes no spool (see Limitations).

### Consumer: `async-result` on a running job

`async-result {job_id}` on a running job grows from
`{job_id, status: "running", started}` to:

    {
      "job_id": "just-us-agents.run-recipe-b496fe63",
      "tool": "just-us-agents.run-recipe",
      "status": "running",
      "started": "2026-06-08T00:12:31Z",
      "elapsed_sec": 312,
      "last_activity": "2026-06-08T00:17:40Z",
      "spool_bytes": 48211,
      "tail": "…last 20 lines of interleaved output…"
    }

The spool-derived fields are NOT recomputed by moxy: `async-result` shells
`clown job status <job_id> --json` and merges its `elapsed_sec`,
`last_activity`, `spool_bytes`, `progress`, and `tail` verbatim onto the
in-memory `{job_id, tool, status, started}`, so an agent sees the same field
names and values whether it probes via `async-result` or `clown job status`
— the channel is the single source of truth (RFC-0010 §3). When the probe
errors (channel disabled, child-server tool, a locally-minted id with no
journal, or an installed clown without the probe) the spool-derived fields
are omitted and the response is exactly the v1 shape. **Terminal** jobs now
also read through the journal: `async-result` takes the terminal record's
`result_ref` (a `madder://blobs/<digest>` URI) and fetches the full stored
result, falling back to the in-memory index when the journal is unavailable
(see FDR-0004, journal-as-state-authority). So both the running probe and the
terminal result are journal-backed, and `async-result` now resolves jobs
launched by another session too (moxy#321), not only its own.

Cross-producer and cross-session consumers may also use the channel-owned
probe directly — `clown job status <job_id>` reports the same fields from the
same files (RFC-0010 §3). `async-result` is moxy's MCP façade over the journal,
not a second source of truth.

## Examples

Dispatch a long recipe, probe it mid-flight, get woken as before:

    async {tool: "just-us-agents.run-recipe", args: {recipe: "test-bats-tag", args: ["grit"]}}
    → {"job_id": "just-us-agents.run-recipe-b496fe63", "status": "running"}

    ... minutes pass, no wake yet — is it alive? ...

    async-result {job_id: "just-us-agents.run-recipe-b496fe63"}
    → {"status": "running", "elapsed_sec": 312,
       "last_activity": "2026-06-08T00:17:40Z", "spool_bytes": 48211,
       "tail": "moxy-bats-grit> ok 24 grit_diff_stat_only\n..."}

    ... fresh last_activity + advancing tail ⇒ leave it alone ...

The same job, probed from a shell or another session:

    clown job status just-us-agents.run-recipe-b496fe63
    → job just-us-agents.run-recipe-b496fe63 (moxy): running, elapsed 5m12s, last activity 4s ago
      --- tail ---
      moxy-bats-grit> ok 24 grit_diff_stat_only
      ...

## Limitations

- **Child MCP server tools have no tail.** Their dispatch is a JSON-RPC
  request/response with no incremental output; the probe still shows state
  and elapsed, but `last_activity` stays at the journal's `started` record
  and no spool is written. Surfacing child-server MCP progress
  notifications as spool lines is deliberate future work.
- **No spool without clown.** When `clown job spool-path` yields nothing
  (channel disabled, clown absent), running `async-result` responses keep
  the v1 shape. A moxy-private fallback spool was considered and rejected:
  it would re-create exactly the per-producer divergence RFC-0010 removes.
- **Stale tails on wedged children survive.** The tee observes the same
  pipes the dispatch blocks on, so a child whose descendants hold the pipes
  (#322/#344) shows a frozen `last_activity` — which is precisely the
  death signal the agent needs, but the job still needs #344/#345 to be
  killable/bounded.
- **The spool is not the result.** Terminal results travel via the madder
  store exactly as in FDR-0004; the spool is reaped by clown's GC and MUST
  NOT be parsed for outcomes.

## Tuning Levers

| Lever | Current | Rationale | Change signal |
|---|---|---|---|
| tail line count | 20 | matches spinclass's proven 15±, fits a probe response | agents routinely follow up with manual spool reads |
| tail read window | 64 KiB from EOF | probe cost must not scale with spool size | tails truncated mid-context on real jobs |
| spool size cap | none (writer-side) | bats/nix logs are tens of MiB at worst; GC bounds lifetime | spool growth complaints, disk pressure in `$XDG_STATE_HOME` |
| spool flush | per-write, no fsync | liveness signal needs freshness, not durability | mtime observed stale while child is verifiably writing |

## More Information

- moxy#341 (feature request), moxy#344/#345 (kill-tree + timeouts — same
  exec-layer neighborhood, sequenced separately)
- clown RFC-0010 (`docs/rfcs/0010-job-output-spool-and-status.md` in clown)
  — the normative spool + probe contract; clown#117 (lifecycle-ownership
  exploration this slices)
- FDR-0004 (`0004-async-tool-dispatch.md`) — the async dispatch design this
  extends; its "No progress events" limitation is superseded by the spool
  for native-moxin jobs
- spinclass `internal/job` — prior art (job.log, mtime-as-last-activity,
  15-line tail)
