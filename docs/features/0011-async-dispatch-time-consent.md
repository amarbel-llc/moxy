---
status: proposed
date: 2026-07-09
promotion-criteria: proposed → experimental once the bare-`async` hook-peek +
  preflight relaxation lands with a bats repro (async{chix.develop-run}
  backgrounds after one consent; deny / permit-async=false still reject
  synchronously). experimental → testing once the hook and preflight are
  confirmed to agree on the inner-tool classification under restart / reload
  (the cross-process consistency risk this design carries). Batch parity (#404)
  and the elicitation alternative (#403) are explicitly out of scope for
  promotion of THIS record.
---

# Async dispatch-time consent for no-perm-request tools

## Problem Statement

FDR 0004's async preflight backgrounds a tool call only if its permission
resolves to `allow`; `ask`, `deny`, and **Unknown** (no moxin `perms-request`
at all) are all rejected synchronously, on the stated rationale that "there is
no client to prompt once the call is detached." But Unknown is not the same as
`ask`: an Unknown tool run *synchronously* is not force-rejected — moxy's hook
falls through and the client applies its own default (often a prompt, often
allow). The result is an asymmetry: `chix.develop-run` and `chix.build` (and
every child-MCP-server tool and moxy builtin — all Unknown) run fine
synchronously but cannot be backgrounded, even though backgrounding is exactly
what a long build/deploy *needs* to escape the ~300s synchronous MCP window
(#356, #370). The fix is to obtain the one required consent *at dispatch time*,
while the client is still attached, rather than reject.

## Interface

The change is a matched pair across two moxy surfaces; neither works without
the other.

### 1. PreToolUse hook peeks at the inner tool

Today the hook auto-allows the `async` builtin outright (`builtinAutoAllow`),
on FDR 0004's premise that async "can only launch work that would never have
prompted." That premise is false for Unknown inner tools. Instead, when the
hooked tool is `async`, the hook reads `tool_input.tool` (the inner tool, in
wire form e.g. `just-us-agents_run-recipe`; Claude Code populates the full
`async` arguments in `tool_input` — verified against real hook logs), converts
it to canonical `server.tool` form, and resolves *that inner tool* through the
same `permcheck.Resolver` it already uses for direct calls. It then maps:

- inner `allow` → **allow** the async call (unchanged fast path).
- inner `ask` **or Unknown** → **`ask`**: force a client consent prompt, with
  `permissionDecisionReason` naming the inner tool and an args summary (e.g.
  `async will background chix.develop-run {script: "deploy"}`), so the user
  consents to the actual work — not a blind "allow async?".
- inner `deny` → **deny**.

The consent reason is the only lever the hook has over the prompt text, so it
carries the informed-consent payload.

### 2. Async preflight admits Unknown

`Proxy.HandleAsync`'s preflight changes from "resolve != allow → reject" to:
`deny` and `permit-async = false` remain hard synchronous rejects; `allow`
backgrounds as before; **`ask`/Unknown are admitted**, because by the time the
`async` call reaches moxy the hook has already forced (and the user has
granted) consent while attached. The preflight no longer re-derives a veto from
Unknown — doing so would waste the consent the hook just obtained.

### Why the pair is coupled (and the risk it carries)

The hook process (`moxy hook`) and the MCP server process both run a
`permcheck.Resolver` and must agree on the inner tool's classification: the
hook decides whether to prompt; the server decides whether to background. If
they disagree (e.g. one has a stale resolver after `restart`/`Proxy.Reload` —
see the known non-invalidation noted in `hook.go`), a consent could be granted
that the server then rejects, or vice versa. This two-process split is the
central fragility of this design and the motivation for the elicitation
alternative (#403), which would let the single server process own the whole
decision.

## Examples

Background a long deploy that has no perms-request (Unknown). Before this
record:

    async {tool: "chix.develop-run", args: {script: "deploy"}}
    → error: async requires the call to resolve to allow;
      chix.develop-run resolved to  (no moxin perm-request for chix.develop-run)

After:

    async {tool: "chix.develop-run", args: {script: "deploy"}}
    → [client consent prompt] "async will background chix.develop-run
       {script: \"deploy\"} — allow?"
    → (user allows)
    → {"job_id": "chix.develop-run-9f2c…", "status": "running"}

An always-allow inner tool still backgrounds with no prompt (unchanged):

    async {tool: "chix.build", args: {installable: ".#default"}}
    → {"job_id": "chix.build-1a2b…", "status": "running"}

A denied inner tool is still rejected synchronously, and `permit-async = false`
still wins regardless of permission.

## Limitations

- **First cut is bare `async` only.** `batch {async: true}` — which must
  resolve and summarize a *list* of inner sub-calls in one consent — is tracked
  separately (#404) and is out of scope here.
- **Consent is only as informative as the client renders.** The hook supplies
  the inner tool + args in `permissionDecisionReason`, but how prominently the
  client shows that reason is client-dependent; on a client that hides it, the
  user sees a less-informed "allow async?".
- **Two-process consistency is assumed, not enforced.** The hook and server
  resolvers can drift (notably across `restart`/`Proxy.Reload`, which does not
  currently invalidate the hook's cached resolver). A drift can waste a consent
  or reject an allowed call. #403 (mid-dispatch elicitation) exists to remove
  this split entirely.
- **This is a genuine revision of FDR 0004's stated posture.** FDR 0004 lists
  "Ask-gated tools cannot run async … a safety posture, not a gap to fill." This
  record narrows that: Unknown (and, via forced consent, `ask`) become
  backgroundable *with* an attached-client consent. `deny` and
  `permit-async = false` remain absolute. The safety property is preserved
  (destructive child tools still get a human gate) — it just moves from
  "reject" to "consent at dispatch."

## More Information

- **Revises** FDR 0004 (async tool dispatch), specifically its
  Eligibility pre-resolution (allow-only) and the "Ask-gated tools cannot run
  async" limitation. FDR 0004 remains the record for the dispatch/journal/store
  mechanics; this record supersedes only its permission-gate posture for
  Unknown/ask inner tools.
- moxy#356 (chix_build rejected with empty resolution), moxy#370 (general
  no-perm-request backgrounding, chix.develop-run motivation) — the bugs this
  fixes.
- moxy#403 — mid-dispatch elicitation alternative (single-process consent;
  would obsolete the hook-peek split).
- moxy#404 — `batch {async:true}` parity follow-up.
- Precedent: the hook's existing `ask`/`deny`/`allow` decision channel
  (`internal/hook/hook.go`, `tryPermsDecision`) — this design routes Unknown
  through the same `ask` channel instead of falling through.
