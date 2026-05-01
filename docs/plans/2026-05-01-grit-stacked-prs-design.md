# Grit stacked-PR workflow support — design

Issue: [#215](https://github.com/amarbel-llc/moxy/issues/215)

## Problem

`grit` exposes enough git surface for linear forward work but not for stacked
PRs. Sessions doing stack work fall back to bash for `git rebase --onto`,
`git rebase -i --autosquash`, `git rebase --update-refs`, and
`git push --force-with-lease`. None of these have grit equivalents today,
so the agent has to ask the user to run them by hand.

## Goals

1. Make `git rebase`'s stack-relevant flags accessible from grit.
2. Replace the unsafe `force` push with `--force-with-lease`.
3. Provide a single tool to push a whole stack at once.
4. Provide an opinionated `restack` verb that bundles the common combo.

## Non-goals

- Stack discovery via metadata (branch descriptions, `.git/stack-config`).
  The caller passes branches explicitly.
- Conflict-resolution heuristics during restack. If `restack` fails, the
  caller falls back to `grit.rebase --continue`/`--abort`.
- A `grit.stack-status` companion. Useful later, not load-bearing.

## Surface changes

### `grit.rebase` (modify)

Three new optional inputs:

- `onto: string` → `git rebase --onto <onto> <upstream> [branch]`
- `autosquash: bool` → `-i --autosquash`
- `update_refs: bool` → `--update-refs`

`GIT_SEQUENCE_EDITOR=true` is already exported by `bin/rebase`, which is
exactly the trick autosquash needs to skip the interactive editor while
still using the autosquash plan. No new env wiring required.

The main/master safety guard is preserved.

### `grit.push` (modify, breaking)

`force` is removed. `force_with_lease: bool` replaces it.
`--force-with-lease` is strictly safer: it refuses the push if the remote
has moved beyond the local ref's last-known position. Any legitimate use
of `--force` is covered by `--force-with-lease`.

The main/master block continues to apply: even with
`force_with_lease=true`, pushing to main/master is rejected.

This is a breaking change. Rationale: `force` is a footgun in the only
workflow (stacks) where it sees frequent use; keeping both creates an
ongoing-deprecation tax with no migration story.

### `grit.restack` (new)

Inputs: `{onto: string (required), root: string (required), repo_path?: string}`.

Behavior: `git rebase -i --autosquash --update-refs --onto <onto> <root>`.
No optional flags. If a caller wants to skip autosquash or update-refs,
they reach for `grit.rebase` instead. Tight contract on purpose.

**Implementation note:** The script pre-resolves `root` to a SHA via
`git rev-parse --verify <root>` and passes `${root_sha}^` to git rebase,
so `<root>` is the *inclusive* bottom of the stack — its own commit is
replayed and its branch ref is repositioned by `--update-refs`. Without
this translation, callers who pass `pr-a` (intending pr-a's tip to be
replayed) would hit git's exclusive-upstream convention and skip
pr-a's commit; callers who pre-decrement (`pr-a^`) would hit a
double-decrement. Pre-resolving to a SHA also gives a clear error
("cannot resolve root ref: <name>") on bad refs, instead of git's
opaque "ambiguous argument" wording.

`continue`/`abort`/`skip` are NOT on this tool. A failed restack is
resumed via `grit.rebase --continue` (or aborted via `--abort`). The
restack tool's job is to start the operation; the rebase tool already
knows how to drive it to completion.

### `grit.push-stack` (new)

Inputs: `{branches: array of string (required, ordered bottom→top), remote?: string=origin, repo_path?: string}`.

Behavior:

1. **Dry-run pre-flight**: for each branch, run
   `git push --dry-run --force-with-lease <remote> <branch>`.
2. If any dry-run fails → return JSON `{phase: "dry-run", results: [...]}`,
   exit non-zero, no real pushes performed.
3. **Real push**: for each branch, run `git push --force-with-lease`.
   Stop on first failure. Return JSON
   `{phase: "push", results: [{branch, status, reason?}]}` where status
   is `ok`, `rejected`, or `skipped`.

The dry-run + stop-on-first-failure combo gives "atomic on the second
wet run" semantics: cheap pre-flight catches the obvious failures; the
real loop fails fast with a debuggable per-branch summary. Git's
dry-run isn't a perfect simulation (race window between dry-run and
real push), so we deliberately avoid promising true atomicity.

Result-type stays `text` (matches existing grit moxins) but the body is
a JSON object the agent can `jq` against.

## Implementation choice: bun for `push-stack`, bash for the rest

`bin/rebase`, `bin/push`, and `bin/restack` are simple flag composers and
stay in bash. Existing grit moxins are all bash; mechanical flag additions
shouldn't break that pattern.

`push-stack` is different: it (a) parses a JSON array argument, (b) passes
caller-controlled branch names to a subprocess, and (c) emits structured
JSON output. CLAUDE.md's bash-vs-bun rule says exactly this combination
warrants bun + zx, because zx's `await $\`git push ${branch}\`` template
interpolation gives correct argv-quoting for free, and JSON I/O is
trivial in TS. So `push-stack` ships as a bun moxin script under
`moxins/grit/src/push-stack.ts`, compiled to `libexec/push-stack` via
`buildBunBinaries` in `flake.nix`.

This is the first bun script in the grit moxin. Other grit moxins keep
their bash form; this introduces the bun toolchain to `grit` only for
the one tool that benefits.

## Testing

All tests are bats integration tests against a real local git repo, per
the existing grit test convention. `zz-tests_bats/common.bash` gets a
new helper:

```bash
setup_stack_fixture() {
  # creates a bare remote and a working repo with three branches
  # A → B → C, each with one commit, all tracking the parent.
  # exports STACK_BRANCHES (space-separated) and STACK_REMOTE.
}
```

Used by both new test files:

- `zz-tests_bats/grit_restack.bats`
  - golden-path autosquash propagation
  - `--onto` move with mismatched bases
  - refusal on main/master
  - conflict path: assert restack fails, then `grit.rebase --abort` cleans up
- `zz-tests_bats/grit_push_stack.bats`
  - clean three-branch push, JSON shape via `jq`
  - dry-run rejection by advancing remote ref to violate the lease
  - real-push mid-stream failure: assert skipped tail in JSON
  - refusal when any branch in the chain is main/master

Tests point `BIN` at `result/share/moxy/moxins/grit/{bin,libexec}/` per
the project rule that bats tests must invoke nix-built wrappers, not raw
source. The `test-bats` recipe's existing `build-go` → `build-moxins`
chain handles the build.

Existing `grit_*.bats` files don't cover rebase/push directly, so the
new flag additions are tested only through the new files. If the
existing test surface grows later, those flags can be retested in place.

## Rollback

The four additive changes (rebase flags + `restack` + `push-stack`) are
free to revert by deleting their TOML/script files and dropping the bun
wiring; nothing else in the codebase depends on them.

The `force` → `force_with_lease` rename is a breaking change. Rollback
procedure: `git revert <merge-commit>`. There is no dual-architecture
period because `force` and `force_with_lease` are conceptually
mutually exclusive — carrying both would defeat the "clean break"
decision and create an ongoing-deprecation tax. Promotion criteria:
none required, since the change is atomic with the merge.

If revert is needed mid-flight (e.g. an agent has been calling
`grit.push --force_with_lease=true` and we want `force` back), the
revert restores `force` and the affected agents can be restarted to
pick up the old TOML schema.

## Out of scope

Captured here so future readers don't re-litigate:

- Stack discovery from upstream tracking refs or merge-base. Caller
  passes `branches` explicitly. If a `grit.stack-status` tool gets
  added later, it can encapsulate the discovery logic.
- Atomic multi-branch push. Git's protocol doesn't support it; the
  dry-run + stop-on-first-failure semantics are the closest we can
  get without a server-side atomic-ref-update extension.
- Auto-deriving `root` for `restack` from the upstream chain. Same
  reason as stack discovery: caller-owned.
