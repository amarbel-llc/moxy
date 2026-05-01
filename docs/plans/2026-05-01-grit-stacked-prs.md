# Grit stacked-PR workflow Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development to implement this plan task-by-task.

**Goal:** Add stack-aware grit verbs (`onto`/`autosquash`/`update_refs` on
`grit.rebase`, `force_with_lease` on `grit.push`, new `grit.restack`, new
`grit.push-stack`) so a session can drive a stacked-PR workflow without
falling back to bash.

**Architecture:** Four bash moxin scripts (rebase additions, push rename,
restack new) and one bun/zx moxin script (push-stack) under
`moxins/grit/`. Convert the grit moxin from `mkMoxin` to `mkBunMoxin` in
`flake.nix` to introduce the bun toolchain to grit; existing bash
binaries continue to work because `mkBunMoxin` includes both `bin/` raw
scripts and compiled bun entrypoints in the same output. Tests are bats
integration tests sharing a `setup_stack_fixture` helper in
`zz-tests_bats/common.bash`.

**Tech Stack:** Bash, bun + zx (TypeScript), nix (gomod2nix +
buildBunBinaries), bats (with bats-assert/island/emo), TOML moxin
manifests, MCP JSON-RPC.

**Rollback:** `force` → `force_with_lease` is the only breaking change.
Rollback is `git revert <merge-commit>`. The four other surfaces are
purely additive and revertible by deleting their files; the `mkMoxin`
→ `mkBunMoxin` conversion is mechanical and revertible by reverting
the `flake.nix` hunk.

**Design doc:** `docs/plans/2026-05-01-grit-stacked-prs-design.md`

**Issue:** [#215](https://github.com/amarbel-llc/moxy/issues/215)

---

## Pre-flight

Confirm baseline before touching anything:

```bash
just test-go
just test-bats
```

Expected: green. If anything is red on a fresh tree, stop and ask the
user before proceeding.

---

## Task 1: Add `onto`/`autosquash`/`update_refs` to `grit.rebase`

**Promotion criteria:** N/A (additive)

**Files:**
- Modify: `moxins/grit/rebase.toml`
- Modify: `moxins/grit/bin/rebase`
- Test: `zz-tests_bats/grit_rebase.bats` (new)

**Step 1: Write the failing tests**

Create `zz-tests_bats/grit_rebase.bats`:

```bash
#!/usr/bin/env bats

load 'common'

BIN="$BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin"

setup() {
  TMPDIR_TEST=$(mktemp -d)
  cd "$TMPDIR_TEST"
  git init -q -b main
  git config user.email t@t
  git config user.name t
  git commit --allow-empty -m base -q
  git checkout -q -b feat
  git commit --allow-empty -m c1 -q
  git commit --allow-empty -m "fixup! c1" -q
}

teardown() {
  rm -rf "$TMPDIR_TEST"
}

@test "rebase --autosquash collapses fixup commits" {
  run "$BIN/rebase" main "" "" "" "" "" true false "$TMPDIR_TEST"
  # arg-order: upstream branch autostash continue abort skip onto autosquash update_refs repo_path
  # → upstream=main, autosquash=true
  assert_success
  run git log --oneline main..feat
  assert_success
  # fixup squashed → only one commit above main
  assert_equal "$(echo "$output" | wc -l | tr -d ' ')" "1"
}

@test "rebase --update-refs flag is accepted" {
  # smoke-test: invocation succeeds even with no intermediate refs to move
  run "$BIN/rebase" main "" "" "" "" "" "" "" true "$TMPDIR_TEST"
  assert_success
}

@test "rebase --onto moves the base" {
  git checkout -q main
  git commit --allow-empty -m main2 -q
  git checkout -q feat
  # rebase feat from old-main onto new main HEAD
  old_main=$(git rev-parse main~1)
  run "$BIN/rebase" "$old_main" "" "" "" "" "" main "" "" "$TMPDIR_TEST"
  # arg-order: upstream=$old_main, onto=main
  assert_success
}
```

**Step 2: Run tests, verify they fail**

```bash
just build-go && just build-moxins
just test-bats-file grit_rebase.bats
```

Expected: FAIL — script doesn't accept the new positional arguments yet
(it ignores them silently or errors on unbound variable).

**Step 3: Update `moxins/grit/rebase.toml`**

Replace the file with:

```toml
schema = 3
result-type = "text"
perms-request = "always-allow"
description = "Rebase current branch onto another ref"
command = "@BIN@/rebase"
arg-order = ["upstream", "branch", "autostash", "continue", "abort", "skip", "onto", "autosquash", "update_refs", "repo_path"]

[input]
type = "object"

[input.properties.upstream]
type = "string"
description = "Ref to rebase onto (branch, tag, commit)"

[input.properties.branch]
type = "string"
description = "Branch to rebase (defaults to current branch)"

[input.properties.autostash]
type = "boolean"
description = "Automatically stash/unstash uncommitted changes"

[input.properties."continue"]
type = "boolean"
description = "Continue rebase after resolving conflicts"

[input.properties.abort]
type = "boolean"
description = "Abort current rebase operation"

[input.properties.skip]
type = "boolean"
description = "Skip current commit and continue rebase"

[input.properties.onto]
type = "string"
description = "Graft the current branch onto this ref (passed to git rebase --onto). Use when an upstream branch's hashes have changed."

[input.properties.autosquash]
type = "boolean"
description = "Squash fixup!/squash! commits into their targets (-i --autosquash, runs non-interactively)."

[input.properties.update_refs]
type = "boolean"
description = "Reposition any intermediate branch refs touched by the rebase (--update-refs). Required for stacked-PR workflows."

[input.properties.repo_path]
type = "string"
description = "Path to the git repository (defaults to current working directory — almost never needed)"
```

**Step 4: Update `moxins/grit/bin/rebase`**

Replace with:

```bash
#!/usr/bin/env bash
set -euo pipefail
# Prevent git from opening an editor (no TTY available in MCP context).
# `true` exits 0, accepting the default commit message.
# GIT_SEQUENCE_EDITOR=true also makes -i --autosquash non-interactive:
# autosquash needs the interactive plan, but exiting 0 accepts the
# auto-generated todo list verbatim.
export GIT_EDITOR=true
export GIT_SEQUENCE_EDITOR=true
upstream="${1:-}"
branch="${2:-}"
autostash="${3:-}"
do_continue="${4:-}"
do_abort="${5:-}"
do_skip="${6:-}"
onto="${7:-}"
autosquash="${8:-}"
update_refs="${9:-}"
repo="${10:-.}"
cd "$repo"

# Safety: block rebase on main/master
current=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)
target="${branch:-$current}"
if [ "$target" = "main" ] || [ "$target" = "master" ]; then
  echo "ERROR: rebasing main/master is blocked for safety" >&2
  exit 1
fi

if [ "$do_continue" = "true" ]; then
  git rebase --continue
elif [ "$do_abort" = "true" ]; then
  git rebase --abort
elif [ "$do_skip" = "true" ]; then
  git rebase --skip
else
  git_args=(rebase)

  [ "$autostash" = "true" ] && git_args+=(--autostash)
  [ "$autosquash" = "true" ] && git_args+=(-i --autosquash)
  [ "$update_refs" = "true" ] && git_args+=(--update-refs)
  [ -n "$onto" ] && git_args+=(--onto "$onto")

  [ -n "$upstream" ] && git_args+=("$upstream")
  [ -n "$branch" ] && git_args+=("$branch")
  git "${git_args[@]}"
fi
```

**Step 5: Rebuild and run tests**

```bash
just build-go && just build-moxins
just test-bats-file grit_rebase.bats
```

Expected: PASS.

**Step 6: Commit**

```bash
git add moxins/grit/rebase.toml moxins/grit/bin/rebase zz-tests_bats/grit_rebase.bats
git commit -m "feat(grit): add onto/autosquash/update_refs to rebase

Refs #215"
```

---

## Task 2: Replace `force` with `force_with_lease` on `grit.push`

**Promotion criteria:** Merge atomicity — `force` and `force_with_lease`
do not coexist in the released tool surface. After merge, callers must
use `force_with_lease`. No dual-architecture period.

**Files:**
- Modify: `moxins/grit/push.toml`
- Modify: `moxins/grit/bin/push`
- Test: `zz-tests_bats/grit_push.bats` (new)

**Step 1: Write the failing test**

Create `zz-tests_bats/grit_push.bats`:

```bash
#!/usr/bin/env bats

load 'common'

BIN="$BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin"

setup() {
  TMPDIR_TEST=$(mktemp -d)
  REMOTE="$TMPDIR_TEST/remote.git"
  WORK="$TMPDIR_TEST/work"
  git init -q --bare "$REMOTE"
  git init -q -b main "$WORK"
  cd "$WORK"
  git config user.email t@t
  git config user.name t
  git remote add origin "$REMOTE"
  git commit --allow-empty -m base -q
  git push -q -u origin main
  git checkout -q -b feat
  git commit --allow-empty -m c1 -q
  git push -q -u origin feat
}

teardown() {
  rm -rf "$TMPDIR_TEST"
}

@test "push --force-with-lease succeeds when remote matches local" {
  cd "$WORK"
  git commit --amend --allow-empty -m c1-amended -q
  run "$BIN/push" "origin" "feat" "" true "$WORK"
  # arg-order: remote branch set_upstream force_with_lease repo_path
  assert_success
}

@test "push --force-with-lease blocks main/master" {
  cd "$WORK"
  run "$BIN/push" "origin" "main" "" true "$WORK"
  assert_failure
  assert_output --partial "force push to main/master is blocked"
}

@test "push --force-with-lease rejects when remote has moved" {
  # advance the remote ref out from under us
  cd "$WORK"
  git clone -q "$REMOTE" "$TMPDIR_TEST/other"
  (cd "$TMPDIR_TEST/other" && git config user.email t@t && git config user.name t \
    && git checkout -q feat && git commit --allow-empty -m sneak -q && git push -q origin feat)
  git commit --amend --allow-empty -m c1-amended -q
  run "$BIN/push" "origin" "feat" "" true "$WORK"
  assert_failure
}
```

**Step 2: Run test, verify it fails**

```bash
just build-go && just build-moxins
just test-bats-file grit_push.bats
```

Expected: FAIL — `force_with_lease` arg position is currently the old
`force` flag's slot and the script still emits `--force`.

**Step 3: Update `moxins/grit/push.toml`**

Replace `[input.properties.force]` with:

```toml
[input.properties.force_with_lease]
type = "boolean"
description = "Force-push with lease (--force-with-lease). Refuses the push if the remote has moved beyond the local ref's last-known position. Safer than --force; use this for stacked-PR workflows."
```

And update `arg-order`:

```toml
arg-order = ["remote", "branch", "set_upstream", "force_with_lease", "repo_path"]
```

**Step 4: Update `moxins/grit/bin/push`**

Replace with:

```bash
#!/usr/bin/env bash
set -euo pipefail
remote="${1:-}"
branch="${2:-}"
set_upstream="${3:-}"
force_with_lease="${4:-}"
repo="${5:-.}"
cd "$repo"

if [ "$force_with_lease" = "true" ]; then
  target="$branch"
  if [ -z "$target" ]; then
    target=$(git rev-parse --abbrev-ref HEAD)
  fi
  if [ "$target" = "main" ] || [ "$target" = "master" ]; then
    echo "ERROR: force push to main/master is blocked for safety" >&2
    exit 1
  fi
fi

git_args=(push)

[ "$force_with_lease" = "true" ] && git_args+=(--force-with-lease)
[ "$set_upstream" = "true" ] && git_args+=(-u)

# Default remote to origin when a branch is specified, so git doesn't
# interpret the branch name as a remote.
if [ -n "$branch" ] && [ -z "$remote" ]; then
  remote="origin"
fi

[ -n "$remote" ] && git_args+=("$remote")
[ -n "$branch" ] && git_args+=("$branch")
git "${git_args[@]}"
```

**Step 5: Rebuild and run tests**

```bash
just build-go && just build-moxins
just test-bats-file grit_push.bats
```

Expected: PASS.

**Step 6: Commit**

```bash
git add moxins/grit/push.toml moxins/grit/bin/push zz-tests_bats/grit_push.bats
git commit -m "feat(grit)!: replace push --force with --force-with-lease

BREAKING CHANGE: grit.push no longer accepts the \`force\` input.
Callers must use \`force_with_lease\` instead. --force-with-lease
is strictly safer in stacked-PR workflows because it refuses the
push when the remote has moved beyond what the caller last saw.

Refs #215"
```

---

## Task 3: Add `setup_stack_fixture` helper to `common.bash`

**Promotion criteria:** N/A

**Files:**
- Modify: `zz-tests_bats/common.bash`

**Step 1: Append helper**

At the bottom of `zz-tests_bats/common.bash`, add:

```bash
# Build a three-branch stack against a local bare remote. Layout:
#   main → A → B → C   (each branch tracks its parent, one commit each)
# Exports:
#   STACK_REMOTE       — bare repo path
#   STACK_WORK         — working repo path
#   STACK_BRANCH_A,_B,_C — branch names
# Caller is responsible for cd into STACK_WORK and for cleanup of the
# enclosing tmpdir.
setup_stack_fixture() {
  local root="$1"
  STACK_REMOTE="$root/remote.git"
  STACK_WORK="$root/work"
  STACK_BRANCH_A="pr-a"
  STACK_BRANCH_B="pr-b"
  STACK_BRANCH_C="pr-c"
  git init -q --bare "$STACK_REMOTE"
  git init -q -b main "$STACK_WORK"
  (
    cd "$STACK_WORK"
    git config user.email t@t
    git config user.name t
    git remote add origin "$STACK_REMOTE"
    git commit --allow-empty -m base -q
    git push -q -u origin main

    git checkout -q -b "$STACK_BRANCH_A"
    git commit --allow-empty -m a1 -q
    git push -q -u origin "$STACK_BRANCH_A"

    git checkout -q -b "$STACK_BRANCH_B"
    git commit --allow-empty -m b1 -q
    git push -q -u origin "$STACK_BRANCH_B"

    git checkout -q -b "$STACK_BRANCH_C"
    git commit --allow-empty -m c1 -q
    git push -q -u origin "$STACK_BRANCH_C"
  )
  export STACK_REMOTE STACK_WORK STACK_BRANCH_A STACK_BRANCH_B STACK_BRANCH_C
}
```

**Step 2: Verify common.bash still loads**

```bash
just test-bats-file grit_diff.bats
```

Expected: PASS (unrelated test, just checking we didn't break the
shared loader).

**Step 3: Commit**

```bash
git add zz-tests_bats/common.bash
git commit -m "test: add setup_stack_fixture helper for stacked-PR tests

Refs #215"
```

---

## Task 4: Add `grit.restack` (bash)

**Promotion criteria:** N/A (additive)

**Files:**
- Create: `moxins/grit/restack.toml`
- Create: `moxins/grit/bin/restack`
- Test: `zz-tests_bats/grit_restack.bats` (new)

**Step 1: Write the failing tests**

Create `zz-tests_bats/grit_restack.bats`:

```bash
#!/usr/bin/env bats

load 'common'

BIN="$BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin"

setup() {
  TMPDIR_TEST=$(mktemp -d)
  setup_stack_fixture "$TMPDIR_TEST"
  cd "$STACK_WORK"
}

teardown() {
  rm -rf "$TMPDIR_TEST"
}

@test "restack autosquashes a fixup against an ancestor branch" {
  # add a fixup on branch C targeting branch A's commit
  a_sha=$(git rev-parse "$STACK_BRANCH_A")
  git checkout -q "$STACK_BRANCH_C"
  git commit --allow-empty --fixup="$a_sha" -q

  run "$BIN/restack" main "$STACK_BRANCH_A" "$STACK_WORK"
  assert_success

  # branch A now contains the fixup absorbed into a1; B and C still chain
  run git log --oneline "main..$STACK_BRANCH_A"
  assert_success
  # exactly one commit above main on branch A (fixup squashed)
  assert_equal "$(echo "$output" | wc -l | tr -d ' ')" "1"
}

@test "restack refuses to run on main" {
  git checkout -q main
  run "$BIN/restack" main main "$STACK_WORK"
  assert_failure
  assert_output --partial "restacking main/master is blocked"
}

@test "restack errors when onto is missing" {
  run "$BIN/restack" "" "$STACK_BRANCH_A" "$STACK_WORK"
  assert_failure
}

@test "restack errors when root is missing" {
  run "$BIN/restack" main "" "$STACK_WORK"
  assert_failure
}
```

**Step 2: Run tests, verify they fail**

```bash
just build-go && just build-moxins
just test-bats-file grit_restack.bats
```

Expected: FAIL — `restack` binary doesn't exist.

**Step 3: Create `moxins/grit/restack.toml`**

```toml
schema = 3
result-type = "text"
perms-request = "always-allow"
description = "Restack a stacked-PR branch chain. Runs `git rebase -i --autosquash --update-refs --onto <onto> <root>` non-interactively. If the rebase fails (conflicts, unfinished operation), resume via grit.rebase --continue or --abort."
command = "@BIN@/restack"
arg-order = ["onto", "root", "repo_path"]

[input]
type = "object"

[input.properties.onto]
type = "string"
description = "New base ref to graft the stack onto (branch, tag, or commit)."

[input.properties.root]
type = "string"
description = "Lower bound of the rebase — typically the bottom of your stack. Commits between <root> and HEAD are replayed onto <onto>."

[input.properties.repo_path]
type = "string"
description = "Path to the git repository (defaults to current working directory — almost never needed)"

[input.required]
0 = "onto"
1 = "root"
```

Note: TOML syntax for required-array — use the standard pattern from
existing moxins. If grit moxins use a different shape (verify by
grepping `required` under `moxins/grit/`), match the existing
convention. If none use `required`, omit the `[input.required]` block
and let the script error on missing args (the script already does so
via `${1:?...}`).

Verification step:

```bash
rg -l '\[input.required\]' moxins/grit/
```

If empty, omit `[input.required]` — the script's `:?` gates suffice.
If present, mirror the existing form.

**Step 4: Create `moxins/grit/bin/restack`**

```bash
#!/usr/bin/env bash
set -euo pipefail
# Non-interactive: `true` exits 0, which accepts the rebase plan
# (including the autosquash todo list) verbatim with no edits.
export GIT_EDITOR=true
export GIT_SEQUENCE_EDITOR=true
onto="${1:?onto is required}"
root="${2:?root is required}"
repo="${3:-.}"
cd "$repo"

current=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)
if [ "$current" = "main" ] || [ "$current" = "master" ]; then
  echo "ERROR: restacking main/master is blocked for safety" >&2
  exit 1
fi

git rebase -i --autosquash --update-refs --onto "$onto" "$root"
```

Make it executable:

```bash
chmod +x moxins/grit/bin/restack
```

**Step 5: Rebuild and run tests**

```bash
just build-go && just build-moxins
just test-bats-file grit_restack.bats
```

Expected: PASS.

**Step 6: Commit**

```bash
git add moxins/grit/restack.toml moxins/grit/bin/restack zz-tests_bats/grit_restack.bats
git commit -m "feat(grit): add restack tool for stacked-PR workflows

restack runs \`git rebase -i --autosquash --update-refs --onto\` in one
shot, the canonical recipe for repositioning a stack after upstream
changes or in-stack fixups. Conflicts fall back to grit.rebase
--continue / --abort.

Refs #215"
```

---

## Task 5: Convert grit moxin to `mkBunMoxin` (no behavior change)

**Promotion criteria:** N/A (mechanical)

**Files:**
- Modify: `flake.nix`

This task introduces the bun toolchain to the grit moxin without adding
any bun scripts yet. Doing it as its own commit keeps the diff readable
and lets us verify all existing grit tests still pass before piling
push-stack on top.

**Step 1: Locate the existing grit-moxin definition**

```bash
rg -n 'grit-moxin' flake.nix
```

Expected: a single line in the moxin let-bindings, currently
`grit-moxin = mkMoxin "grit" [ ] { pathMode = "inherit"; };`.

**Step 2: Convert to `mkBunMoxin`**

Replace that line with:

```nix
        grit-moxin = mkBunMoxin "grit" [ ] {
          # No bun entrypoints yet — added in the next task.
        } { pathMode = "inherit"; };
```

If the empty bun-entrypoints attrset breaks `mkBunMoxin` (e.g.
`buildBunBinaries` requires at least one), include push-stack in this
task and merge with Task 6. Verify by checking `mkBunMoxin`'s
implementation:

```bash
rg -n -A 30 'mkBunMoxin = ' flake.nix
```

If an empty attrset is rejected, do this task and Task 6 in a single
commit instead.

**Step 3: Rebuild and run all grit-related bats tests**

```bash
just build-go && just build-moxins
just test-bats-file grit_diff.bats
just test-bats-file grit_tag.bats
just test-bats-file grit_rebase.bats
just test-bats-file grit_push.bats
just test-bats-file grit_restack.bats
```

Expected: all PASS. The bash binaries should land at
`result/share/moxy/moxins/grit/bin/` exactly as before.

**Step 4: Commit**

```bash
git add flake.nix
git commit -m "build(grit): convert moxin to mkBunMoxin (no behavior change)

Sets up grit to host bun/zx scripts alongside its existing bash
binaries. No new tools introduced in this commit — push-stack lands
next.

Refs #215"
```

If you had to merge with Task 6, skip Task 6's separate commit step.

---

## Task 6: Add `grit.push-stack` (bun/zx)

**Promotion criteria:** N/A (additive)

**Files:**
- Create: `moxins/grit/src/push-stack.ts`
- Create: `moxins/grit/push-stack.toml`
- Modify: `flake.nix` (add the entrypoint)
- Test: `zz-tests_bats/grit_push_stack.bats` (new)

**Step 1: Write the failing tests**

Create `zz-tests_bats/grit_push_stack.bats`:

```bash
#!/usr/bin/env bats

load 'common'

BIN="$BATS_TEST_DIRNAME/../result/share/moxy/moxins/grit/bin"

setup() {
  TMPDIR_TEST=$(mktemp -d)
  setup_stack_fixture "$TMPDIR_TEST"
  cd "$STACK_WORK"
}

teardown() {
  rm -rf "$TMPDIR_TEST"
}

@test "push-stack pushes a clean three-branch chain" {
  # amend each branch so each push needs --force-with-lease
  for b in "$STACK_BRANCH_A" "$STACK_BRANCH_B" "$STACK_BRANCH_C"; do
    git checkout -q "$b"
    git commit --amend --allow-empty -m "${b}-amended" -q
  done

  branches_json=$(jq -cn --arg a "$STACK_BRANCH_A" --arg b "$STACK_BRANCH_B" --arg c "$STACK_BRANCH_C" \
    '[$a, $b, $c]')
  run "$BIN/push-stack" "$branches_json" origin "$STACK_WORK"
  assert_success
  echo "$output" | jq -e '.phase == "push"'
  echo "$output" | jq -e '.results | length == 3'
  echo "$output" | jq -e '[.results[].status] | all(. == "ok")'
}

@test "push-stack dry-run rejects when remote has diverged" {
  # advance branch B's remote ref out from under us
  git clone -q "$STACK_REMOTE" "$TMPDIR_TEST/other"
  (cd "$TMPDIR_TEST/other"
    git config user.email t@t && git config user.name t
    git checkout -q "$STACK_BRANCH_B"
    git commit --allow-empty -m sneak -q
    git push -q origin "$STACK_BRANCH_B")

  for b in "$STACK_BRANCH_A" "$STACK_BRANCH_B" "$STACK_BRANCH_C"; do
    git checkout -q "$b"
    git commit --amend --allow-empty -m "${b}-amended" -q
  done

  branches_json=$(jq -cn --arg a "$STACK_BRANCH_A" --arg b "$STACK_BRANCH_B" --arg c "$STACK_BRANCH_C" \
    '[$a, $b, $c]')
  run "$BIN/push-stack" "$branches_json" origin "$STACK_WORK"
  assert_failure
  echo "$output" | jq -e '.phase == "dry-run"'
  echo "$output" | jq -e '.results[] | select(.branch == $b and .status == "rejected")' --arg b "$STACK_BRANCH_B"

  # confirm no real pushes happened: remote tip on A still matches its pre-test state
  remote_a=$(cd "$STACK_REMOTE" && git rev-parse "refs/heads/$STACK_BRANCH_A")
  local_a=$(git rev-parse "$STACK_BRANCH_A")
  [ "$remote_a" != "$local_a" ]
}

@test "push-stack rejects main/master in branches list" {
  branches_json=$(jq -cn --arg a "$STACK_BRANCH_A" '[$a, "main"]')
  run "$BIN/push-stack" "$branches_json" origin "$STACK_WORK"
  assert_failure
  assert_output --partial "main/master"
}
```

**Step 2: Run tests, verify they fail**

```bash
just build-go && just build-moxins
just test-bats-file grit_push_stack.bats
```

Expected: FAIL — `push-stack` binary doesn't exist.

**Step 3: Create `moxins/grit/src/push-stack.ts`**

```typescript
#!/usr/bin/env bun
// Push a stacked branch chain with --force-with-lease.
// Two phases: dry-run pre-flight (atomic gate), then real pushes
// stop-on-first-failure with a per-branch JSON summary.
import { $ } from "zx";
$.verbose = false;

type Status = "ok" | "rejected" | "skipped";
type Result = { branch: string; status: Status; reason?: string };

const [branchesJson, remoteArg, repo] = process.argv.slice(2);
const remote = remoteArg && remoteArg.length > 0 ? remoteArg : "origin";

if (!branchesJson) {
  console.error("ERROR: branches argument is required (JSON array)");
  process.exit(2);
}

let branches: string[];
try {
  branches = JSON.parse(branchesJson);
  if (!Array.isArray(branches) || branches.some(b => typeof b !== "string")) {
    throw new Error("branches must be an array of strings");
  }
} catch (e) {
  console.error(`ERROR: invalid branches JSON: ${(e as Error).message}`);
  process.exit(2);
}

if (repo) process.chdir(repo);

// Reject any main/master in the chain — same guard as grit.push, applied
// up front so we never start pushing a stack that contains a forbidden ref.
for (const b of branches) {
  if (b === "main" || b === "master") {
    console.error("ERROR: force push to main/master is blocked for safety");
    process.exit(1);
  }
}

const dryRun: Result[] = [];
for (const branch of branches) {
  try {
    await $`git push --dry-run --force-with-lease ${remote} ${branch}`;
    dryRun.push({ branch, status: "ok" });
  } catch (e: any) {
    dryRun.push({
      branch,
      status: "rejected",
      reason: (e.stderr?.toString() ?? e.message ?? "").trim(),
    });
    // mark remaining as skipped for observability
    for (const b of branches.slice(dryRun.length)) {
      dryRun.push({ branch: b, status: "skipped" });
    }
    console.log(JSON.stringify({ phase: "dry-run", results: dryRun }, null, 2));
    process.exit(1);
  }
}

const real: Result[] = [];
for (const branch of branches) {
  try {
    await $`git push --force-with-lease ${remote} ${branch}`;
    real.push({ branch, status: "ok" });
  } catch (e: any) {
    real.push({
      branch,
      status: "rejected",
      reason: (e.stderr?.toString() ?? e.message ?? "").trim(),
    });
    for (const b of branches.slice(real.length)) {
      real.push({ branch: b, status: "skipped" });
    }
    console.log(JSON.stringify({ phase: "push", results: real }, null, 2));
    process.exit(1);
  }
}
console.log(JSON.stringify({ phase: "push", results: real }, null, 2));
```

**Step 4: Create `moxins/grit/push-stack.toml`**

```toml
schema = 3
result-type = "text"
description = "Push a stacked branch chain with --force-with-lease. Runs a dry-run pre-flight across every branch first; if any dry-run fails, no real pushes happen. Otherwise pushes branches in order, stopping at the first failure. Returns a JSON object: {phase: 'dry-run'|'push', results: [{branch, status: 'ok'|'rejected'|'skipped', reason?}]}."
command = "@BIN@/push-stack"
arg-order = ["branches", "remote", "repo_path"]

[input]
type = "object"

[input.properties.branches]
type = "array"
description = "Branches to push, ordered bottom→top. Each is pushed independently with --force-with-lease."

[input.properties.branches.items]
type = "string"

[input.properties.remote]
type = "string"
description = "Remote name (default origin)"

[input.properties.repo_path]
type = "string"
description = "Path to the git repository (defaults to current working directory — almost never needed)"

[annotations]
open-world-hint = true
```

**Step 5: Wire into `flake.nix`**

Update the `grit-moxin` definition to include the push-stack
entrypoint:

```nix
        grit-moxin = mkBunMoxin "grit" [ ] {
          "push-stack" = "moxins/grit/src/push-stack.ts";
        } { pathMode = "inherit"; };
```

`mkBunMoxin`'s fileset includes `moxins/<name>/src/*.ts` as part of
`rawSrc` (already verified by inspecting the helper). The compiled
output lands at `$out/share/moxy/moxins/grit/bin/push-stack` — same
directory as the bash scripts.

**Step 6: Stage new files for nix to see them**

`nix build` against a dirty tree only sees git-tracked files (per
CLAUDE.md). Stage but don't commit yet:

```bash
git add moxins/grit/src/push-stack.ts moxins/grit/push-stack.toml flake.nix
```

**Step 7: Rebuild and run tests**

```bash
just build-go && just build-moxins
just test-bats-file grit_push_stack.bats
```

Expected: PASS. If the bun build complains about a missing dep
(e.g. `zx`), check the existing chix bun moxins — they pull `zx` and
`@types/bun` from the repo-level `package.json`/`bun.lock` rather than
per-moxin. The grit conversion to `mkBunMoxin` should pick those up.

**Step 8: Commit**

```bash
git add zz-tests_bats/grit_push_stack.bats
git commit -m "feat(grit): add push-stack tool

push-stack pushes a stacked-PR branch chain in two phases:
1. Dry-run pre-flight across all branches; abort if any would fail.
2. Real push, stopping at first failure.

Output is a JSON object the agent can parse to drive recovery.
Implemented in bun/zx because the script (a) parses a JSON array
input, (b) passes caller-controlled branch names to git, and (c)
emits structured output — exactly the case where zx's argv-quoting
beats bash arrays + jq.

Refs #215"
```

---

## Task 7: Update tool docs / discovery surfaces

**Promotion criteria:** N/A

**Files:**
- Verify: `CLAUDE.md` for any grit references that need updating
- Verify: `README.md` if it lists grit tools
- Verify: nothing else hard-codes the `force` flag name

**Step 1: Search for `grit_force` or `grit.push.*force`**

```bash
rg -n 'grit.*force[^_]' --glob '!*.lock' --glob '!result' --glob '!.tmp' .
rg -n '"force"' moxins/grit/ docs/ README.md CLAUDE.md 2>/dev/null
```

Expected: nothing user-facing. If a doc mentions the old `force` flag
in instructions or examples, update it to `force_with_lease` in the
same commit.

**Step 2: Run `moxy status` against the worktree to verify the schema**

```bash
just build-go
moxy status
```

Expected: grit's tool list shows `restack` and `push-stack`,
`rebase` shows the new flags in its schema, `push` shows
`force_with_lease` instead of `force`. No errors.

**Step 3: Run the full test matrix**

```bash
just test
```

Expected: green. If anything unrelated broke, stop and inspect — do
not paper over with `--continue` or `-k`.

**Step 4: Commit any doc updates**

If `rg` turned up doc references, fix them and commit:

```bash
git add <files>
git commit -m "docs: update grit references to force_with_lease

Refs #215"
```

If nothing needed updating, skip the commit.

---

## Task 8: Subagent simplify pass

**Promotion criteria:** N/A

**Step 1: Invoke the simplify skill in a subagent**

Per the user's request, run the `simplify` skill on the changed code
in this branch. Pass it the list of files we touched:

```
moxins/grit/rebase.toml
moxins/grit/bin/rebase
moxins/grit/push.toml
moxins/grit/bin/push
moxins/grit/restack.toml
moxins/grit/bin/restack
moxins/grit/src/push-stack.ts
moxins/grit/push-stack.toml
flake.nix (the grit-moxin hunk)
zz-tests_bats/common.bash (setup_stack_fixture)
zz-tests_bats/grit_rebase.bats
zz-tests_bats/grit_push.bats
zz-tests_bats/grit_restack.bats
zz-tests_bats/grit_push_stack.bats
```

Use the Agent tool with subagent_type=general-purpose and a prompt
that tells it: this is a stacked-PR feature for grit, simplify pass
on the diff vs `master`, focus on duplication between scripts (e.g.
the main/master guard appears in 3 places — is it worth extracting?),
JSON-output construction in push-stack, and bats fixture clarity. Do
NOT loosen safety guards, do NOT consolidate the rebase + restack
scripts (the design deliberately splits them).

**Step 2: Apply simplifications selectively**

Review the subagent's output. Apply the changes that:
- preserve every test
- don't introduce a new file dependency between moxins
- don't move logic from grit's bash/ts scripts into moxy core

Skip suggestions that:
- merge `restack` back into `rebase` (design decision, see design doc)
- replace bun with bash (design decision, see CLAUDE.md rule)
- broaden the main/master guard's match (e.g. to all "default branches")
  without explicit user buy-in

**Step 3: Re-run tests**

```bash
just test
```

Expected: green.

**Step 4: Commit any simplifications**

```bash
git commit -am "refactor(grit): simplify after subagent pass

Refs #215"
```

If no changes were applied, skip.

---

## Task 9: Subagent code review

**Promotion criteria:** N/A

**Step 1: Invoke the eng:code-reviewer subagent**

Use the Agent tool with `subagent_type=eng:code-reviewer`. Prompt:

> Review the diff between `master` and the current branch for the grit
> stacked-PR feature (issue #215). Focus on: (1) the breaking change
> from `force` to `force_with_lease` — is the safety guard preserved?
> (2) push-stack's two-phase dry-run/real flow — are there race
> windows or unhandled error paths? (3) restack's non-interactive
> autosquash via `GIT_SEQUENCE_EDITOR=true` — could this silently
> accept a destructive plan? (4) the bats fixture — does it cover
> the failure modes we care about?
>
> Skip: style nits, naming bikeshed, suggestions that contradict
> the design doc at `docs/plans/2026-05-01-grit-stacked-prs-design.md`.

**Step 2: Triage findings**

For each issue the reviewer raises:
- **High-confidence bug** → fix immediately, add a regression test if
  the existing tests don't cover the case.
- **Design pushback** → if the reviewer disagrees with a design
  decision, defer to the design doc. If the reviewer raises a case
  the design doc didn't consider, surface it to the user before
  changing anything.
- **Style / nit** → ignore unless the reviewer flags it as
  high-priority.

**Step 3: Re-run tests after any fixes**

```bash
just test
```

Expected: green.

**Step 4: Commit**

```bash
git commit -am "fix(grit): address review feedback

Refs #215"
```

If no changes, skip.

---

## Task 10: Merge

**Step 1: Final smoke test**

```bash
just
```

Expected: build + test green.

**Step 2: Merge via spinclass**

```bash
mcp__spinclass__merge-this-session  with git_sync=true
```

Per CLAUDE.md, this is the create-PR-and-merge flow for spinclass
sessions. The commit message body should reference `Closes #215` so
GitHub auto-closes the issue on merge.

If the merge tool reports a non-fast-forward (someone else pushed
to master), pull, rebase, re-run `just test`, then re-merge.

**Step 3: Verify**

After merge:
- `gh issue view 215` — should be closed
- `moxy status` (against master worktree) — should show
  `grit.restack`, `grit.push-stack`, and the new schema for rebase
  and push.

---

## Notes for the implementer

- **CLAUDE.md says `direnv reload` doesn't work mid-session.** If you
  need to pick up new flake inputs (you don't, for this work), exit
  and re-enter the session. The `mkMoxin` → `mkBunMoxin` change
  doesn't touch flake inputs, so this won't bite you.
- **`nix build` only sees git-tracked files.** New `.ts` and `.toml`
  files must be `git add`-ed before `just build-moxins`. This is
  Task 6 Step 6 — don't skip it.
- **Don't drop the safety guards.** Every rebase/push/restack script
  has a main/master block. If you're tempted to simplify by removing
  it, don't — the guard is the only line preventing the agent from
  force-pushing trunk.
- **Don't add a `--force` escape hatch.** The design deliberately
  removes `force`. If a use case for it surfaces, file an issue.
- **The `GIT_SEQUENCE_EDITOR=true` trick is load-bearing for
  autosquash.** Without it, `-i --autosquash` opens an editor and
  hangs (no TTY). Don't replace it with `:` — `true` and `:` behave
  identically here, but `true` is what the existing rebase script
  uses.
- **Bun startup is fine.** ~36ms per the bun fork's measurement
  (referenced in CLAUDE.md). Don't pre-optimize push-stack to bash
  for performance reasons.
