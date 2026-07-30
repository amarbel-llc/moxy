---
status: proposed
date: 2026-07-30
promotion-criteria: grit.stash (the prototype), grit.branch, grit.stage, and
  grit.worktree all ship as collapsed family tools; the old per-verb tools
  (branch-create, branch-delete, reset, hard-reset, checkout, add, rm, mv,
  stash-save, stash-apply, stash-drop, worktree-list, worktree-remove) are gone
  from tools/list; each family's dynamic-perms gate reproduces the pre-collapse
  per-verb permission levels (verified by the bats-grit lane); and no follow-up
  is needed to re-split a family because a verb landed under the wrong capability
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

### `grit.branch` — controls the HEAD / branch pointer

Everything that moves *which commit a branch points at* or *which branch HEAD
points at*.

| `subcommand` | Was | Perms |
|---|---|---|
| `create` | `branch-create` | allow |
| `delete` | `branch-delete` (keeps main/master block) | ask |
| `soft` | `reset --soft <ref>` (move HEAD, keep index + tree) | ask |
| `hard` | `hard-reset <ref>` (move HEAD, discard tree; main/master block) | ask |
| `checkout` | `checkout` (switch which branch HEAD tracks) | allow |

### `grit.stage` — controls the index / stage

Everything that changes *what is staged for the next commit*, including the file
operations that stage their own effect.

| `subcommand` | Was | Perms |
|---|---|---|
| `add` | `add` | allow |
| `unstage` | `reset HEAD [-- <paths>]` (the non-pointer half of old `reset`) | allow |
| `rm` | `rm` (deletes tracked files) | ask |
| `mv` | `mv` | allow |

### `grit.worktree` — list / remove worktrees

A straight mechanical collapse (no redesign), included because it is a clean
two-verb family.

| `subcommand` | Was | Perms |
|---|---|---|
| `list` | `worktree-list` | allow |
| `remove` | `worktree-remove` | ask |

### Dissolved tools

`reset` and `hard-reset` **cease to exist as tools**; their behavior is split by
capability — the pointer-moving modes (`--soft`, `--hard`) become `grit.branch`
subcommands, the index-only mode (`reset HEAD`) becomes `grit.stage unstage`.
`branch-create`, `branch-delete`, `add`, `rm`, `mv`, `checkout`,
`worktree-list`, `worktree-remove`, and the three `stash-*` tools likewise
disappear as top-level names, folded into their family.

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

    grit.branch { "subcommand": "soft", "ref": "HEAD~1" }

Discard all working-tree changes back to origin/main (prompts; blocked on
main/master):

    grit.branch { "subcommand": "hard", "ref": "origin/main" }

Unstage one file (index only — working tree untouched):

    grit.stage { "subcommand": "unstage", "paths": ["flake.lock"] }

Stage everything, then remove a tracked file:

    grit.stage { "subcommand": "add", "paths": ["."] }
    grit.stage { "subcommand": "rm", "paths": ["old.go"] }   # prompts

## Limitations

- **Per-subcommand required args are enforced in the dispatcher, not the
  schema.** A union schema cannot say "`ref` is required for `hard` but not for
  `checkout`" — `required` can only name args needed by *every* subcommand. Verbs
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
- **`grit.remote` is out of scope here.** fetch/pull/push/push-stack are a
  separate family (Phase 2b) complicated by `push-stack` being a compiled bun
  `.ts` binary rather than a bash script, so its dispatcher branch must `exec`
  the bun binary. Deferred to its own record/pass.

## More Information

- `grit.tag` (`moxins/grit/tag.toml` + `bin/tag` + `bin/tag-perms`) — the
  pre-existing prototype of the union-`subcommand` + dynamic-perms shape every
  family here follows.
- FDR 0007 (`0007-name-template.md`) — the configurable name template that a
  later phase will use to switch grit (and every server) from the dot separator
  to hyphenated names; orthogonal to this capability regrouping.
