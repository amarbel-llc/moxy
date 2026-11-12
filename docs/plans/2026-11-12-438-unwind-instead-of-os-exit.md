# moxy#438: unwind instead of os.Exit (needs an operator ruling)

Status: question for the operator. No code change; the stopgap
(`stderrlog.OriginalStderr()`, merged earlier) keeps a fatal startup error
visible.

## State

- The sidestep prints the final `moxy: %v` straight to the pre-tee stderr, so
  a fatal startup error is visible again (lane `startup_errors`).
- Still open, as the issue says: failure does not *unwind*. `os.Exit` skips
  deferred cleanup and does not wait for the stderr copier goroutine, so the
  banners can still be cut off and the final line never reaches the session
  log.
- Direct exit sites in moxy today (`rg 'os\.Exit|log\.Fatal'`):
  - `cmd/moxy/main.go`: `os.Exit(status.Run(...))` inside a `RunCLI` closure
    (below `main`, nothing above it unwinds); `log.Fatalf` ×2 (install-mcp,
    generate-plugin); the final `os.Exit(exitUnreadable)` and `os.Exit(1)`.
  - `cmd/moxy/narrowing.go`: `os.Exit(runNarrows(...))` in a closure.
  - `cmd/toolbox/main.go`, `cmd/readguard/main.go`: small single-purpose
    binaries; their exits sit at the top and nothing is deferred.

## Questions

1. **Adopt dewey's `errors.ActiveContext` for the command layer, or fix with
   plain Go unwinding first?**
   Recommended: **plain unwinding first, `ActiveContext` later if wanted.**
   The goal (flushes and deferred cleanup run) needs only that every closure
   return an error or an exit code to a single `func main() { os.Exit(run()) }`
   tail call. `ActiveContext` is a larger design change (the issue's author
   says its API was not studied) and can be layered on afterwards without
   undoing this.

2. **How should "exit with this code, no message" travel up (`status`,
   `narrows`)?**
   Recommended: **a small `exitCode` error type** that `run()` maps to the
   process exit code without printing, so `status` and `narrows` keep their
   documented exit codes (`narrows` reserves 1 for "the child widens").

3. **Drain the stderr pipe on exit, or keep the sidestep?**
   Recommended: **drain.** `stderrlog` should close the write end and wait
   for the copier with a short timeout once `run()` returns, then the final
   error line can go through the normal path and reach the session log; the
   sidestep and its `TODO(#438)` go away. The hazard noted in the issue is real
   (a child holding the write end would hang an unbounded wait), hence the
   timeout; first confirm whether spawned children inherit `os.Stderr`.

4. **Ban direct `os.Exit` with a lint (purse-first#186)?**
   Recommended: **yes, once 1 to 3 land**; the analyzer is another repo's
   work, so it is not part of this issue.

## Not started because

Items 1 and 3 choose between designs and touch `cmd/moxy/main.go` and
`internal/stderrlog`, which a startup-path change elsewhere could also touch.
Say which way to go and it is one merge.
