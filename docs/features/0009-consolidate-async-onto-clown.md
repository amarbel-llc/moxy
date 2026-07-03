---
status: exploring
date: 2026-07-03
promotion-criteria: exploring → proposed once the maintainer picks a target
  shape (full-drop vs. thin-forwarder) and the madder-coupling question is
  resolved; proposed → experimental once the chosen surface is implemented and
  FDR-0004 is amended/superseded accordingly
---

# Consolidate async dispatch onto clown's job platform

## Problem Statement

moxy ships its own `async` / `async-result` / `async-cancel` meta tools
(FDR-0004, `accepted`) to background a proxied tool call. But the job lifecycle
they manage is **already clown's**: the ids are clown-managed, the wakeup rides
clown's channel, and after the RFC-0015 migration `async-result` literally
shells `ringmaster read` / `ringmaster status` and `async-cancel` maps onto
`ringmaster cancel`. Maintaining a second surface over one job model is
duplication — two things to document, divergent semantics, and every new job
primitive (e.g. the `ringmaster wait` blocking join) risks being mirrored twice
(#384). The open question is *how much* of moxy's async surface can retire onto
clown, given that one piece — resolving the tool **result** — is moxy/madder
specific and clown does not currently touch madder.

## What is genuinely moxy-specific vs. duplicated

The surface splits cleanly into two halves:

**Moxy-specific (cannot move to clown as-is):**
- **The runner.** `async` dispatches a *proxied tool call* on a detached
  context through moxy's own pipeline (permission preflight, statsd, the native
  exec layer with its process-group kill and spool tee). `ringmaster start`
  opens a job record; it does not know how to run a moxy tool.
- **The result blob.** `async` marshals the `ToolCallResultV1` to the
  user-level **moxy-async madder store** and references it from the clown `done`
  record as `result_ref = madder://blobs/<digest>`. `async-result` fetches that
  blob back. clown's journal stores `result_ref` as an **opaque string**
  (RFC-0009) — it never resolves it.

**Duplicated / now a façade over clown (candidates to retire):**
- `async-result`'s *status* path → `ringmaster status` (elapsed/spool/tail).
- `async-result`'s *journal* read → `ringmaster read`.
- `async-cancel` → `ringmaster cancel`.
- the blocking join, never in moxy, → `ringmaster wait` (the clown#154 gap,
  now **landed** via RFC-0015 — so #384's sequencing precondition is cleared).

## Design options under consideration

### Option 1 — Full drop; clown/ringmaster gains madder-blob resolution

Retire all three moxy tools. `ringmaster read` (or a new verb) **resolves the
`result_ref`** and inlines the `madder://blobs/<digest>` payload, so clown is
the single surface end-to-end.

- **Pro:** one surface, zero moxy async tools, no divergence risk.
- **Con:** couples **clown → madder** for *every* producer, not just moxy —
  clown must bundle/known madder and resolve arbitrary producers'
  `result_ref` schemes. That inverts RFC-0009's deliberate "result_ref is an
  opaque producer-owned pointer" contract. It also still leaves the **runner**
  problem: something must dispatch the moxy tool, so `async`-as-launcher likely
  survives regardless.

### Option 2 — Thin forwarder (recommended)

Keep only what is genuinely moxy's:
- **`async`** stays — the launcher/runner of a proxied call, returning the
  clown-managed job id.
- **`async-result`** slims to *result-blob resolution only* (fetch the
  `ToolCallResultV1` for a job id from the moxy-async store). Its status/journal
  paths already defer to `ringmaster`; drop the moxy-specific dressing and point
  agents at `ringmaster status` / `ringmaster read` / `ringmaster wait` for
  lifecycle.
- **`async-cancel`** is **retired** in favor of `ringmaster cancel`.

- **Pro:** the madder coupling stays in **moxy**, where the storage choice was
  made; clown keeps `result_ref` opaque (RFC-0009 intact). Removes the
  genuinely-duplicated cancel/status surface. Minimal, honest surface: moxy owns
  only "launch a proxied call" + "hand back its result blob".
- **Con:** two tools remain (a launcher + a result-fetcher), so the surface is
  not *zero*; agents use ringmaster for lifecycle and moxy for launch+result.

### Option 3 — Status quo (rejected baseline)

Keep all three moxy tools as-is. Rejected: it is the duplication #384 exists to
remove, and it re-mirrors every future clown job primitive.

## Recommendation

**Option 2 (thin forwarder).** It removes the genuinely-duplicated surface
(cancel, status-as-a-moxy-tool) while keeping the madder coupling on moxy's side
of the boundary, honoring RFC-0009's opaque-`result_ref` contract. Option 1 is
warranted only if there is a *broader, cross-producer* need for clown itself to
inline result blobs — i.e. a deliberate decision that clown should depend on
madder for all producers, not just moxy. That is a clown-side architectural call
(an RFC against clown), not a moxy feature decision, and should be made
explicitly rather than backed into by moxy's cleanup.

## Relationship to FDR-0004

This **reshapes an `accepted` feature**. Whichever option lands, FDR-0004 must be
amended (or superseded): under Option 2 its `async-cancel` tool and the
`async-result` status-merge (FDR-0005) are removed and their behavior delegated
to `ringmaster`; under Option 1 the whole tool trio retires. No implementation
should proceed until that FDR-0004 revision is agreed, since it changes a
promoted contract.

## Limitations

- This FDR records the *decision to be made*, not an implemented feature. It is
  `exploring` until the maintainer selects a target shape.
- The runner (`async`) is out of scope for retirement under any option — clown
  cannot dispatch a moxy proxied tool call.

## More Information

- Issue [#384](https://github.com/amarbel-llc/moxy/issues/384) — the
  consolidation proposal this FDR works through.
- [FDR-0004](0004-async-tool-dispatch.md) — the async dispatch surface this
  reshapes (`accepted`).
- [FDR-0005](0005-async-job-live-status.md) — the `async-result` status merge
  that Option 2 delegates to `ringmaster status`.
- clown RFC-0015 (ringmaster/troupe platform binaries) — the CLI surface async
  now shells; `ringmaster wait` clears clown#154's blocking-join gap.
- clown RFC-0009 (job-wakeup channel) — the `result_ref`-is-opaque contract the
  madder-coupling question turns on.
