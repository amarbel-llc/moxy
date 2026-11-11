# moxy#518: toolbox.run over /proc/<pid>/cmdline (needs an operator ruling)

Status: question for the operator. No behaviour change made.

## What happened

The prompt in #518 came from a research subagent that ran `toolbox.run` with
file operands under `/proc/<pid>/cmdline` (`strings -n 1 /proc/1707527/cmdline
... | cut -c1-160`, about twenty pids). Those operands are outside the read
policy: the approved `/proc` entries are the six anchors from #465, not
arbitrary per-process paths. The designed `ask` applied. It is not a bug in
the floor: a pipeline over stdin, a `madder://` operand or in-CWD files gets
`allow` (pinned by `aecd703`), and only an operand outside the policy asks.

## Questions

1. **Close #518 as works-as-designed?**
   Recommended: **yes.** The expectation in the issue (a pure pipeline over
   already-blessed inputs does not prompt) already holds; the prompt came from
   an operand that is not blessed.

2. **Should `/proc/<pid>/cmdline` join the read policy?**
   Recommended: **no.** Command lines carry secrets in arguments (tokens,
   passwords passed as flags), for any process on the host, including other
   users' sessions. Reading them is a policy decision for the operator, not a
   fix, and `env.resolve` already refuses `/proc` outright (moxy#497).
   If the operator wants a narrower grant, the smallest one is
   `/proc/self/*` only (a process inspecting itself), which does not reach
   other pids.

3. **If a research agent legitimately needs process command lines, how?**
   Recommended: a purpose-built tool (for example a read-only `env.ps` that
   prints pid, user and a command line with argument values redacted), so the
   grant is explicit and auditable, rather than widening file reads.

No code is waiting on these answers.
