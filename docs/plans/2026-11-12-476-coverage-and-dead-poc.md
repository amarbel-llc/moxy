# moxy#476: recipes that needed an ambient `go`

Status: done. The POC is deleted and `run-go-cover` is rebuilt on godyn.

## What changed

- `explore-poc-build-dynamic-perms` and `explore-poc-test-dynamic-perms`,
  `cmd/moxy-exporel-dynamic-perms` and `zz-bats_explore/` are deleted. The POC
  proved the `dynamic` perms-request feature; the feature has shipped
  (`internal/native/dynperms.go`, with unit tests and the bats lanes), and the
  POC was never wired into the nix-built source set, so it could not run
  without an ambient `go`.
- The old `run-go-cover` was deleted: it ran `go test -coverprofile` with an
  ambient `go` and a `go.mod`, neither of which exists under go.nix (igloo
  FDR 0008). A new `run-go-cover` replaces it, below.

## Coverage (answered, 2026-11-12)

The coverage question is settled: igloo confirmed that `buildGodynModule` and
`buildGoAuto` take `cover`, `coverMode` (set|count|atomic) and `coverPackages`,
and that with `cover = true` and `tests = true` the result has
`passthru.coverage` (`covdata/`, `coverage.out` in text format, `percent.txt`).
moxy builds that as `moxy-native-cover` (`mkMoxyNative { cover = true;
coverMode = "atomic"; }`) and exposes `.#moxy-coverage`. `just run-go-cover
[substring]` builds it, links `result-coverage`, and prints `percent.txt`
(optionally filtered), for example `just run-go-cover internal/hook` printed
`internal/hook coverage: 79.8% of statements`.

Limits, all godyn(7)'s own: cgo and asm packages are not instrumented,
packages without tests are absent from `percent.txt`, and `buildGoAuto`
forwards `cover` to the godyn backend only. The result is per package, not
per function; `coverage.out` is the text-format profile for a per-function
view with Go's cover tool. No gap beyond those was hit, so there is no igloo
issue to draft. `just debug-go-test PKG PATTERN` still runs one package's
tests.
