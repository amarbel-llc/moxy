# moxy#476: recipes that needed an ambient `go`

Status: done for the POC; one question left for the operator.

## What changed

- `explore-poc-build-dynamic-perms` and `explore-poc-test-dynamic-perms`,
  `cmd/moxy-exporel-dynamic-perms` and `zz-bats_explore/` are deleted. The POC
  proved the `dynamic` perms-request feature; the feature has shipped
  (`internal/native/dynperms.go`, with unit tests and the bats lanes), and the
  POC was never wired into the nix-built source set, so it could not run
  without an ambient `go`.
- `run-go-cover` is deleted. It ran `go test -coverprofile` with an ambient
  `go` and a `go.mod`, neither of which exists under go.nix (igloo FDR 0008),
  so it could not run.

## Question for the operator

1. **Do we want a coverage recipe back, built on godyn?**
   Recommended: **yes, when someone next needs coverage, not now.** The
   shape would be a recipe over godyn's cover options (`buildGoAuto` `cover`,
   `coverMode`, `coverPackages`) that prints per-function coverage for one
   package. Those options are igloo's interface and design space, and no igloo
   session was live to ask, so I did not guess their use. The question for
   igloo is: what is the supported way to get a per-function coverage report
   for one package of a godyn-built module, and does it keep the per-package
   cache?

Until then, `just debug-go-test PKG PATTERN` runs a package's tests.
