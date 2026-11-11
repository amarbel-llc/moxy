# Read tools and symlink leaves (moxy#490 follow-up)

Status: design, operator-ruled 2026-11-11. Implementation follows in the
same series.

## Problem

`folio.read`, `rg.search`, `jq.jq` (and the other path readers) ask the policy
broker about a path with its parents resolved and its leaf as named
(`perms.CanonicalPath`, folio-perms `resolve`). The tool scripts then open the
path *by name*. A symlink leaf that was swapped in, or that points outside the
policy, after the verdict is followed. `toolbox.run` already closes this by
opening each operand itself with `openat2(RESOLVE_NO_SYMLINKS)`
(docs/features/0016-toolbox.md, "Opening").

## Ruling

Refuse symlink leaves, **with an allow-list**: a symlink leaf is allowed only
if its fully resolved target is itself inside the read policy, so ordinary
symlinks within a worktree keep working. Otherwise refuse with an error that
names the path. Also close the swap-after-verdict window: open once and read
the opened file, not the name.

Not in scope (other #490 rulings, undecided): `folio.df` gating, `chix.*`
paths, the load-time error. `folio.glob` is moxy/live-aspen (#514), `--json`
is moxy/noble-redwood (#506).

## Design

One small Go binary, `readguard`, wraps a reader's script. The moxin's
`command` becomes `readguard exec ... -- <script> <args>`; moxy passes the
arguments exactly as before (`arg-order`), so the scripts and their inputs are
unchanged apart from receiving `/dev/fd/N` where they received a path.

`internal/readguard` holds the logic (the gate and the open), shared with
`cmd/toolbox`, whose private gate moves there.

### Opening one operand

1. Absolute-ize against the cwd, then `perms.CanonicalPath` (the path the
   hook judged).
2. Re-ask the gate. Broker when `CLOWN_POLICY_BROKER` answers, else the
   `folio-perms read` floor (exit 0 allow, 1 ask, 2 deny). Only **deny**
   refuses here: an ask was the hook's to put before the call reached us.
3. `openat2(RESOLVE_NO_SYMLINKS)` that path. Success means the file opened is
   the path judged, regardless of any swap since (any symlink, leaf or
   component, fails the open).
4. On `ELOOP` (a symlink leaf, or a component swapped for one): resolve with
   `filepath.EvalSymlinks`, ask the gate about the **target** and require
   **allow** (an ask means outside the policy and is refused: nothing here can
   ask a user, and the ruling says "inside the read policy"). Then open the
   target with `RESOLVE_NO_SYMLINKS` again; a second `ELOOP` (swapped again)
   refuses. The error names the operand, the link target and the reason.
5. `fstat` the opened descriptor; non-regular files are refused (as toolbox).

`/dev/fd/N` (N ≥ 3, moxy's substituted blob pipes) and `madder://` pass
through untouched, as they do today.

### Per tool

| tool | what readguard does |
| --- | --- |
| `folio.read` | opens `file_path`, passes `/dev/fd/N` to the script |
| `jq.jq` | opens each element of `files` (JSON array rewritten to `/dev/fd/N`s); `cwd` is verified, relative files resolve against it |
| `rg.search` | `path` is opened with `--any-args`: a directory is entered by `fchdir` on the opened descriptor and searched as `.` (rg still finds the parents' ignore files), a file arrives as `/dev/fd/N`; the script maps rg's `./` or `/dev/fd/N` prefix back to the caller's name (`READGUARD_ORIG`/`READGUARD_REPL`) |
| `folio.ls`, `tree`, `du`, `file-type` | `--any-args 1`: a directory is entered (`fchdir`) and listed as `.`, a file arrives as `/dev/fd/N`; the script pipes its text (before its JSON envelope) through `$READGUARD_MAP map` to get the caller's name back (`internal/readguard.MapOutput`: `.`, `.:`, `./x`, du's `\t.`, tree's `"name":"."`, or the `/dev/fd/N` token). `du` gets `-D`, `file-type` gets `-L` for the fd form |
| `folio.tar`, `chix.nix-hash` | `--file-args 1`: the file arrives as `/dev/fd/N` (the tar script already copies a `/dev/fd` tarball to a temp file); no name is printed |
| `arboretum.outline`, `arboretum.search` | **verified only** (`--verify-args`): the language is detected from the file extension, which a `/dev/fd/N` stand-in loses, so the tool opens the name itself and the check-to-open gap stays |
| `env.resolve` | **not wrapped, deliberately**: it exists to report a symlink's target (`readlink`) and its canonical form, so refusing or following links would change what it is for. It reads no file content; the existing `/proc`, `/sys`, `/dev` refusal stays. Operator may overrule |
| `arboretum.rewrite` | a write op; not part of this change |

The descriptors are opened in readguard after the inherited ones, cleared of
close-on-exec, and the script is `exec`ed (no extra process; moxy's
process-group kill and wait are unaffected). Numbers are taken from what
`open` returns, so they cannot collide with moxy's substituted blob fds.

### Residuals (stated, not hidden)

- (closed in the follow-up) `rg.search` and `jq`'s `cwd` first only verified
  the name; they now work from the opened descriptor. rg's walk below the root
  does not follow symlinks.
- A parent directory swapped between verdict and open is judged by name, as
  everywhere in the policy; `RESOLVE_NO_SYMLINKS` does catch a parent swapped
  for a *symlink*.
- Non-Linux: `O_NOFOLLOW` covers the leaf only, as in toolbox.

## Tests

- Go (`internal/readguard`): regular file; symlink to an in-policy target
  opens the target and the *target* is what the gate saw; symlink out of
  policy refused, error names the path; symlink to an ask target refused;
  deny on the named path refused; non-regular refused; broken link refused;
  JSON `files` rewriting.
- Bats (`zz-tests_bats/read_symlinks.bats`, through the nix-built moxin
  binaries): `folio.read` of a legitimate in-worktree symlink works; a link
  to a file outside the policy is refused with the path in the error; a path
  that is a plain file when judged and a symlink out of policy when opened
  (swapped) is refused, via the readguard binary with a gate that allows the
  name; same for `jq.jq`.
