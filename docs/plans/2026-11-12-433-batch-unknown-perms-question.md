# moxy#433: should `batch` admit tools with no declared permission? (needs an operator ruling)

Status: question for the operator. No code change.

## What happens today

A synchronous `batch {calls}` resolves every sub-call's tier before running
any (`internal/proxy/batch.go`, `HandleBatch`):

- `allow` and `ask` proceed. The hook's **one** prompt for the `batch` call is
  the consent for the whole batch ("its single prompt is its design contract",
  `internal/hook/hook.go`).
- `deny` and `unknown` abort the batch with a bailout record naming them.
  `unknown` is a tool with no `perms-request`: builtins, child-MCP tools, moxins
  that declare none. Under an enforcing policy (moxy#486) only `allow`, or an
  `enforce-allow` grant lifting `unknown`, proceeds.

The async paths differ on purpose (FDR 0011, moxy#404): `async` and
`batch {async:true}` admit `ask` and `unknown` inner tools, because the hook
resolves each inner tool itself and forces a consent prompt that **names** it
before the call reaches moxy.

## Why this is a permissions question

For a synchronous batch nothing prompts per sub-call. Sub-calls run through
`CallToolV1` inside moxy, after the hook has already answered for the `batch`
call. So if `unknown` is admitted as "round down to ask", the only consent for
that sub-call is the single prompt for the batch, which shows the `calls` list
but is not told that some entries have no declared permission. Today that
class of tool cannot be reached through a batch at all, which is the safe side
of the trade. Under `dontAsk` the batch prompt itself is refused, so this
matters for interactive sessions.

## Questions

1. **Admit `unknown` sub-calls in a synchronous batch?**
   Recommended: **yes, but only with the same informed consent the async path
   gives**, not silently. See 2. If the operator wants the fail-closed
   behaviour kept, answer no and close the issue as "working as designed"
   (the workaround is to call such tools singly, each prompting).

2. **How does consent become informed?**
   Options:
   a. *As the issue proposes*: change the pre-flight to proceed on `unknown`
      and rely on the batch prompt. Simplest; the prompt does not say which
      tools lack a declared permission.
   b. *Recommended*: also make the hook's decision for a synchronous batch
      resolve the inner tools, as `tryBatchAsyncInnerDecision` already does
      for `batch {async:true}`, so a batch with any `ask` or `unknown` member
      prompts with those members **named**, and a batch with a `deny` member is
      refused. This gives the sync and async paths one rule and one prompt
      text, and is the larger change (hook plus pre-flight, bats for both).
   c. Keep the pre-flight allow/ask-only and add an opt-in
      (`batch {allow_unknown:true}`) the caller must set; the prompt then names
      the flag. Smallest surprise, but a new parameter.

3. **Under an enforcing policy (moxy#486), does `unknown` stay refused unless
   an `enforce-allow` grant lifts it?**
   Recommended: **yes, unchanged.** Nobody can answer a prompt there, so
   "round down to ask" would mean "allow"; the grant is the explicit lift.

If 1 and 2b are accepted, the work is one moxy merge: the hook change, the
pre-flight change, tests that fail first for a batch with an `unknown` member
(named in the prompt) and with a `deny` member (refused), and the FDR 0011
text updated.
