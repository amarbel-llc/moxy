# moxy#438: unwind instead of os.Exit

Status: done, 2026-11-12, following the operator's ruling to take the
recommendations below. The `dewey` `ActiveContext` adoption stays a possible
later layer, not part of this.

## The problem

`stderrlog.Init` tees fd 2 into a pipe that a copier goroutine fans out to the
session log and the terminal. A fatal error printed just before `os.Exit` died
in the pipe, because `os.Exit` does not wait for goroutines or run defers. An
earlier stopgap wrote the final line to the pre-tee stderr (`OriginalStderr`),
which fixed the terminal but not the log or the banners.

## What changed

1. **Plain unwinding, not `ActiveContext` first.** `main` is now
   `func main() { os.Exit(run()) }`, the one permitted exit. Every command
   returns an error or an exit code up to `run`: `install-mcp` and
   `generate-plugin` no longer `log.Fatalf`; `status` and `narrows` no longer
   `os.Exit` from inside a `RunCLI` closure.
2. **An `exitCodeError`** (`cmd/moxy/exitcode.go`) carries "exit with this
   code, print nothing" up from `status` and `narrows`, so their documented
   codes stay (`narrows` still reserves 1 for "the child widens" and maps any
   other error to 2). `exitWith(0)` is nil.
3. **Drain, not sidestep.** `stderrlog.Drain(timeout)` puts fd 2 back on the
   original stderr, which drops this process's reference to the pipe, and
   waits for the copier to finish. `run` calls it after the final error line
   and the stopping banner, with a 500 ms bound: a spawned child that still
   runs keeps the pipe open (children inherit fd 2), so the wait is bounded
   and a timeout loses nothing already copied. `OriginalStderr` and its
   `TODO(#438)` are gone, and the final line now reaches the session log too
   (the copier writes to the renamed file's open descriptor).
4. **Lint (purse-first#186)** is another repo's work and still wanted. After
   this change `rg 'os\.Exit|log\.Fatal'` in `cmd/moxy` finds only the tail
   call in `main`; the small `cmd/toolbox` and `cmd/readguard` binaries exit
   at the top of `main` with nothing deferred.

## Tests

- `internal/stderrlog/drain_test.go` re-executes the test binary: the final
  line reaches both the terminal and the session log after `Drain`, and a
  child holding fd 2 open makes `Drain` give up in about 300 ms (false)
  without losing the earlier line. Written before `Drain` existed and failing
  to compile, then passing.
- `cmd/moxy/exitcode_test.go` pins the exit-code mapping, including that a
  `narrows` error is 2, not 1.
- `zz-tests_bats/startup_errors.bats` now asserts the error line and the
  stopping banner are in the completed session log, not only on the terminal.
