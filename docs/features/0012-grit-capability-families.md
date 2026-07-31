---
status: experimental
date: 2026-07-31
promotion-criteria: grit.stash, grit.branch, grit.stage, grit.worktree, and
  grit.remote all ship as collapsed family tools, with grit.branch reaching its
  full noun-centric scope (pointer + history + inspect) and grit.stage owning the
  index mutate+inspect surface; the old per-verb tools (branch-create,
  branch-delete, reset, hard-reset, checkout, add, rm, mv, stash-*, worktree-*,
  fetch, pull, push, push-stack, rebase, restack, cherry-pick, revert, log,
  git-rev-parse, status, diff) are gone from tools/list; the split `diff`
  resolves correctly under both grit.stage and grit.branch with its
  context-appropriate flag subset; each family's dynamic-perms gate reproduces
  the pre-collapse per-verb permission levels (verified by the bats-grit lane);
  and no follow-up is needed to re-split a family because a verb landed under the
  wrong capability
---

# Grit capability-based tool families

## Problem Statement

grit exposes ~29 tools, many of them fine-grained siblings of one git concept
(`branch-create` / `branch-delete`, `stash-save` / `stash-apply` / `stash-drop`,
`reset` / `hard-reset`, `worktree-list` / `worktree-remove`). This floods the
agent's tool list with near-duplicates, spends a separate permission prompt per
verb, and — worse — organizes the surface along git's *command names* rather than
along *what the operation actually touches*, so an agent has to know that
"discard my working-tree changes" lives under `hard-reset` and "unstage a file"
lives under `reset`, two tools whose only kinship is the word "reset".

## Interface

Grit's tools are reorganized into **family tools discriminated by a `subcommand`
arg**, where each family owns one *capability* — a thing git state can be — not
one git subcommand name. The `grit.tag` tool (create/delete/list/push/verify
under one `subcommand`) is the pre-existing prototype of this shape; `grit.stash`
(#—, the spike that motivated this record) is the first deliberate application.

The capability axis, and the families it produces:

### `grit.branch` — the branch as an object: pointer, history, inspection

`grit.branch` is the dominant tool: it owns everything you do *to or with a
branch* — moving its pointer, rewriting its commit history, and inspecting its
state. This is a **noun-centric** axis (the branch is the object) layered on the
capability axis: operations whose subject *is a branch/ref* live here, while
operations on the *index/working tree* live in `grit.stage`.

Pointer (moves which commit a branch points at, or which branch HEAD tracks):

| `subcommand` | Was | Perms |
|---|---|---|
| `create` | `branch-create` | allow |
| `delete` | `branch-delete` (keeps main/master block) | ask |
| `reset-soft` | `reset --soft <ref>` (move HEAD, keep index + tree) | ask |
| `reset-hard` | `hard-reset <ref>` (move HEAD, discard tree; main/master block) | ask |
| `checkout` | `checkout` (switch which branch HEAD tracks) | allow |

History (replay / rewrite the branch's commits):

| `subcommand` | Was | Perms |
|---|---|---|
| `rebase` | `rebase` (incl. `--continue`/`--abort`/`--skip`) | ask |
| `restack` | `restack` (a `rebase -i --autosquash --update-refs` preset) | ask |
| `cherry-pick` | `cherry-pick` (appends new commits, no history rewrite) | allow |
| `revert` | `revert` (appends *new* undo commits, no history rewrite) | allow |

Inspect (read the branch's / ref's state):

| `subcommand` | Was | Perms |
|---|---|---|
| `log` | `log` | allow |
| `rev-parse` | `git-rev-parse` (dropped the `git-` prefix) | allow |
| `diff` | `diff` (ref form — see split `diff` below) | allow |

### `grit.stage` — the index / working tree: mutate and inspect

Everything whose subject is *what is staged for the next commit* or the
*working-tree state* — including the file operations that stage their own effect,
and the reads that report on the index/working tree.

| `subcommand` | Was | Perms |
|---|---|---|
| `add` | `add` | allow |
| `unstage` | `reset HEAD [-- <paths>]` (the non-pointer half of old `reset`) | allow |
| `rm` | `rm` (deletes tracked files) | ask |
| `mv` | `mv` | allow |
| `status` | `status` (working-tree + index state) | allow |
| `diff` | `diff` (index form — see split `diff` below) | allow |

### `grit.worktree` — list / remove worktrees

A straight mechanical collapse (no redesign), included because it is a clean
two-verb family.

| `subcommand` | Was | Perms |
|---|---|---|
| `list` | `worktree-list` | allow |
| `remove` | `worktree-remove` | ask |

### Split `diff` — one operation, two capabilities

`diff` is the one operation that legitimately belongs to *two* capabilities, so
it is exposed as a `subcommand` in **both** families, each surfacing only the
flags meaningful in that context (a first for this codebase — every other
subcommand is unique to one family):

- **`grit.stage { subcommand: "diff" }`** — the *index / working-tree* diff.
  Flags: `staged`, `paths`, `stat_only`, `context_lines`. **No `ref`** —
  comparing against another commit is not a stage operation.
- **`grit.branch { subcommand: "diff" }`** — the *ref-to-ref* diff. Flags:
  `ref`, `paths`, `stat_only`, `context_lines`. **No `staged`** — the staged/
  unstaged distinction is an index concept.

Both dispatch the same underlying `bin/diff` logic; the two families just admit
different flag subsets. The standalone `grit.diff` tool disappears.

### Stays standalone

Not every tool joins a family — forcing one where no shared capability exists is
the premature-abstraction failure this design is meant to avoid:

- **`commit`** — singular and central; no sibling verbs.
- **`merge`** — "combine branches" fits neither the pointer, history-replay, nor
  inspect groupings cleanly, and its `--continue`/`--abort` conflict machinery is
  its own. (Note: `rebase`'s `--continue`/`--abort`/`--skip` live under
  `grit.branch`, so conflict-resolution is split across `grit.branch rebase` and
  standalone `grit.merge` — an accepted mild inconsistency of leaving merge out.)
- **`clone`** — a remote op, but it runs *before* a repo exists (no `repo_path`
  context) and carries a bespoke `clone-perms` destination-safety gate, so it
  does not fold cleanly into `grit.remote`.
- **`tag`** — already the union-`subcommand` prototype this whole design follows.

### Dissolved tools

`reset` and `hard-reset` **cease to exist as tools**; their behavior is split by
capability — the pointer-moving modes (`--soft`, `--hard`) become `grit.branch`
subcommands, the index-only mode (`reset HEAD`) becomes `grit.stage unstage`.
`branch-create`, `branch-delete`, `add`, `rm`, `mv`, `checkout`,
`worktree-list`, `worktree-remove`, the three `stash-*` tools, `fetch`, `pull`,
`push`, `push-stack` (→ `grit.remote`), and — in the branch/stage expansion —
`rebase`, `restack`, `cherry-pick`, `revert`, `log`, `git-rev-parse`, `status`,
and `diff` likewise disappear as top-level names, folded into their family.

### Mechanics (per family)

Each family is pure moxin content — no Go, no `flake.nix` change (mkBunMoxin
copies `moxins/grit/` wholesale and `chmod +x` + `patchShebangs` everything in
`bin/`):

- one union `<family>.toml` — `subcommand` enum required, every verb's args
  flattened into one schema with per-property "which subcommand" descriptions,
  `arg-order` mapping the union positionally onto the dispatcher;
- one `bin/<family>` bash `case "$subcommand"` dispatcher merging the old
  scripts verbatim into branches;
- one `bin/<family>-perms` dynamic-perms gate (`perms-request = "dynamic"`)
  reproducing the pre-collapse per-verb permission level (exit 0 = allow,
  1 = ask, 2 = deny).

The dot separator is preserved (`grit.branch`, `grit.stage`) — the switch to a
hyphenated name template is a later, global, cross-server change (FDR 0007) and
is out of scope here.

## Examples

Move the current branch's HEAD back one commit, keeping the changes staged:

    grit.branch { "subcommand": "reset-soft", "ref": "HEAD~1" }

Discard all working-tree changes back to origin/main (prompts; blocked on
main/master):

    grit.branch { "subcommand": "reset-hard", "ref": "origin/main" }

Unstage one file (index only — working tree untouched):

    grit.stage { "subcommand": "unstage", "paths": ["flake.lock"] }

Stage everything, then remove a tracked file:

    grit.stage { "subcommand": "add", "paths": ["."] }
    grit.stage { "subcommand": "rm", "paths": ["old.go"] }   # prompts

## Limitations

- **Per-subcommand required args are enforced in the dispatcher, not the
  schema.** A union schema cannot say "`ref` is required for `reset-hard` but not
  for `checkout`" — `required` can only name args needed by *every* subcommand. Verbs
  that need an arg validate it in the bash `case` branch and exit non-zero with a
  message, exactly as `grit.tag`'s `require_name` does. This is inherent to the
  flatten-into-one-schema pattern.
- **Wide union schemas.** `grit.branch` and `grit.stage` each carry the union of
  all their verbs' properties; most properties apply to only one subcommand, and
  say so in their description. This is the accepted cost of one tool per
  capability.
- **Blast radius on existing references.** `grit.add`, `grit.rm`, `grit.mv`,
  `grit.checkout`, and the reset tools disappear as top-level names. Allow-rules,
  agent workflows, and docs referencing them by name break and must be updated to
  the family form.
- **`grit.remote` (fetch/pull/push/push-stack).** A shipped family; `push-stack`
  is a compiled bun `.ts` binary rather than a bash script, so its dispatcher
  branch `exec`s the bun binary (which is kept as an entrypoint) instead of
  inlining it.
- **Split-`diff` double-exposure.** Exposing one operation under two families is
  novel here and slightly complicates dispatch (both `bin/stage` and `bin/branch`
  carry a `diff` case into the shared logic). The alternative — a standalone
  `grit.diff` — was rejected to keep the capability axis consistent, at the cost
  of this duplication.

## More Information

- `grit.tag` (`moxins/grit/tag.toml` + `bin/tag` + `bin/tag-perms`) — the
  pre-existing prototype of the union-`subcommand` + dynamic-perms shape every
  family here follows.
- FDR 0007 (`0007-name-template.md`) — the configurable name template that a
  later phase will use to switch grit (and every server) from the dot separator
  to hyphenated names; orthogonal to this capability regrouping.
