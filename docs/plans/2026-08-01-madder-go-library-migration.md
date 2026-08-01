# Migrate moxy from the madder binary to the madder Go library

Replaces `internal/native/madder.go`'s os/exec shell-out to the `madder` CLI
with in-process calls to the madder Go library. Also fixes moxy#11 (the
missing-blob hang) via an in-process existence check, and sidesteps madder#276
(the broken `madder has` CLI).

## API (verified via madder-go-pkgs source)

- Module path: **`github.com/amarbel-llc/madder/go`** (NOT the flake fetch URL
  `code.linenisgreat.com/madder`). All imports are `.../go/pkgs/...` (public;
  never `internal/`).
- The one type moxy needs: `pkgs/blob_store_env.BlobStoreEnv`.
  - `OpenBlob(markl.Id) (BlobReader, error)` — BlobReader is an io.ReadCloser
    (streaming; feeds OpenBlob's pipe without buffering the whole blob).
  - `HasBlobInAnyStore(markl.Id) bool` — the clean in-process existence check
    (the moxy#11 fix; replaces the buggy `madder has`).
  - `GetDefaultBlobStore() BlobStoreInitialized`, `GetDefaultBlobStoreId() string`.
- Write path (no one-call helper): `w := store.MakeBlobWriter(store.GetDefaultHashType())`;
  `io.Copy(w, r)`; `w.Close()`; `digest := w.GetMarklId()`.
- Init chain (per process, ~4 constructors):
  `env_dir.MakeDefault(ctx, {EnvVarNames: madder_env.DefaultEnvVarNames}, "madder")`
  → `env_ui.MakeDefault(...)` → `env_local.Make(uiEnv, dirEnv)`
  → `blob_store_env.MakeBlobStoreEnv(local)`. A non-empty store map (i.e.
  `GetDefaultBlobStoreId() != ""`) is the VerifyDefaultStore equivalent.
- Digest strings (`blake2b256-…`) → `markl.Id` via text-unmarshal.
  **TODO before coding:** confirm the exact parse method name/signature on
  `github.com/amarbel-llc/piggy/go/pkgs/markl.Id` (the agent saw text-marshal
  round-tripping but did not read Set/UnmarshalText's exact signature).

## Design constraint: keep the blobSource/BlobWriter interface

substitute.go depends only on the `blobSource` interface (`OpenBlob(ctx,
digest) (*os.File, BlobWriter, error)`) and `BlobWriter` (Start/Wait/Cleanup).
Keep those interfaces. The new library-backed MadderClient must still return a
`*os.File` read-end + a `BlobWriter` from OpenBlob — i.e. we still create an
os.Pipe and fill it, but from an in-process `io.Copy(pw, libReader)` goroutine
instead of a `madder cat` subprocess. This keeps substitute.go + its tests
untouched and confines the change to madder.go.

## Stages

### Stage 1: wire the dependency (nix + go.mod) — build-only, no behavior

1. Add madder to `goFlakeInputs` in flake.nix (module
   `github.com/amarbel-llc/madder/go`) — remove the "madder is NOT bridged"
   exclusion. Follow how tommy/purse-first are bridged.
2. `go get github.com/amarbel-llc/madder/go@<locked-rev>` (or add the require +
   `go mod tidy`) so go.mod/go.sum resolve out-of-nix too.
3. `just build-gomod2nix` to regenerate gomod2nix.toml with the new (heavy)
   transitive tree (piggy, tommy, dewey, AWS SDK, SFTP, charmbracelet…).
4. `git add` the new/changed files (nix build only sees git-tracked files).
5. Verify `nix build .#moxy` still builds (deps resolve) BEFORE touching
   madder.go. This isolates dependency-wiring failures from code failures.
   NOTE: this stage likely needs the user to run any `direnv reload` if the
   devshell inputs change; flag if so.

### Stage 2: reimplement MadderClient against the library

6. Rewrite madder.go: MadderClient holds a `blob_store_env.BlobStoreEnv` built
   once (lazy, guarded) via the init chain. Reimplement:
   - `Write(ctx, io.Reader) (string, error)` → MakeBlobWriter + io.Copy + Close
     + GetMarklId.
   - `OpenBlob(ctx, digest) (*os.File, BlobWriter, error)` → parse digest→markl.Id;
     `HasBlobInAnyStore` FIRST (fail fast → error if missing — the #11 fix);
     else os.Pipe + a libBlobWriter whose Start() launches a goroutine doing
     `io.Copy(pw, env.OpenBlob(id))` then closes pw; Wait() joins it; Cleanup()
     idempotent.
   - `CatBytes(ctx, digest)` → OpenBlob reader + io.ReadAll (or keep for callers).
   - `VerifyDefaultStore(ctx)` → build env; error if GetDefaultBlobStoreId()=="".
7. Drop the `defaultMadderBin` ldflag + PATH lookup (no binary needed). Remove
   the `-X …defaultMadderBin=` line + the `MADDER_BIN` binary wiring in
   flake.nix if now unused (CHECK: bats lanes may still want a madder binary
   for test setup — keep MADDER_BIN for tests if so, only drop the runtime pin).
8. Keep the `blobSource`/`BlobWriter` interface + substitute.go untouched.

### Stage 3: fix #11 + tests

9. moxy#11: the fail-fast HasBlobInAnyStore in OpenBlob makes the missing-blob
   case return an error immediately (no hanging child). Re-enable / de-flake
   native_exec_resource_missing_cached_id_errors.
10. Add a Go unit test hammering the missing-blob path under `-race` (drive
    OpenBlob with a missing digest against a real/temp store; assert immediate
    error, no hang). Confirms the #11 fix deterministically.
11. Full `just` gate.

## Risks / watch-items

- **Dependency weight**: the AWS SDK / SFTP / charmbracelet trees materially
  grow moxy's closure + build time. Accepted per the decision to proceed.
- **goFlakeInputs mechanics**: bridging a new module can surface version
  conflicts across the shared transitive graph (piggy/tommy are already
  followed for smith/madder in flake.nix inputs — watch for require-version
  skew between the binary-era follows and the now-imported code).
- **markl.Id parse**: confirm the exact API before Stage 2 (TODO above).
- **gomod2nix + untracked files**: `git add` before every `nix build`.
- **Store discovery at runtime**: the library walks the CWD/.madder layout the
  same way the CLI did; confirm the CWD-at-tool-call semantics match (moxy sets
  cmd.Dir per call today — the library env is built in-process, so the store
  discovery CWD is moxy's, not the tool's; VERIFY this matches current behavior,
  since the CLI ran `madder` from moxy's CWD too).
