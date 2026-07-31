# Phase 2d — fold history/reads into grit.branch + grit.stage

Implements the branch-as-dominant-noun placement recorded in FDR 0012. Pure
moxin content — no Go, no flake.nix change (mkBunMoxin copies moxins/grit/
wholesale, chmod +x + patchShebangs every bin/ script).

## Target end state

- **grit.stage** gains subcommands: `status`, `diff` (index form).
- **grit.branch** gains subcommands: `rebase`, `restack`, `cherry-pick`,
  `revert`, `log`, `rev-parse`, `diff` (ref form).
- **grit.diff standalone disappears**; `status`, `log`, `git-rev-parse`,
  `rebase`, `restack`, `cherry-pick`, `revert` standalones disappear.
- **Stays standalone**: `commit`, `merge`, `clone`, `tag`.

## Split diff

One shared implementation. Keep `bin/diff` as an internal helper script (not a
tool — no `diff.toml`). Both `bin/stage` and `bin/branch` have a `diff` case that
invokes `@BIN@/diff` with the flags valid for that context:

- stage diff: `staged`, `paths`, `stat_only`, `context_lines` (no `ref`).
- branch diff: `ref`, `paths`, `stat_only`, `context_lines` (no `staged`).

`bin/diff`'s existing positional arg-order is `ref paths staged stat_only
context_lines repo_path`. Each family's dispatcher fills the slots it exposes and
leaves the others empty. `diff.toml` also has `cache-results = "always"` and no
`result-type` (defaults) — the family `.toml`s already set `result-type = "text"`;
confirm the diff subcommand output still caches acceptably (or accept text).

## Build steps

1. **bin/branch**: add `rebase`/`restack`/`cherry-pick`/`revert`/`log`/
   `rev-parse`/`diff` cases, merging the old scripts verbatim. rebase's
   continue/abort/skip become args. rev-parse drops the `git-` prefix.
2. **bin/branch-perms**: extend — log/rev-parse/diff → allow (0);
   rebase/restack/cherry-pick/revert → ask (1). (revert creates commits;
   still ask for consistency with the other history verbs — revisit if noisy.)
3. **branch.toml**: extend enum + union args + arg-order. This is the wide one
   (~12 subcommands). arg-order must stay in sync with bin/branch positions.
4. **bin/stage**: add `status` + `diff` cases (diff → `@BIN@/diff` with index
   flags). **bin/stage-perms**: status/diff → allow (0).
5. **stage.toml**: extend enum + union args (status: paths/untracked/ignored;
   diff: staged/paths/stat_only/context_lines) + arg-order.
6. **Keep bin/diff, bin/rebase, bin/restack, bin/cherry-pick, bin/revert,
   bin/log, bin/git-rev-parse, bin/status** as internal helpers IF a dispatcher
   execs them; OR inline verbatim and delete. Decision: inline the simple ones
   (status/log/rev-parse/cherry-pick/revert), exec-or-inline rebase/restack
   (restack is itself a rebase wrapper — inline). Keep bin/diff as the shared
   exec target for the split. Delete the corresponding *.toml for every tool
   that stops being a tool.
7. **Delete .toml**: diff, status, log, git-rev-parse, rebase, restack,
   cherry-pick, revert.
8. **Tests**: migrate grit_status/grit_diff/grit_log/grit_rebase/
   grit_cherry-pick/grit_restack/grit_revert bats to the new family+subcommand
   form. GREP FOR DIRECT `$BIN/<tool>` CALLERS (the grit_push.bats lesson) —
   not just `grit.<tool>` MCP names. Add stage status/diff + branch
   log/rev-parse/diff coverage. Verify split diff: stage diff rejects/ignores
   `ref`, branch diff rejects/ignores `staged`.
9. **Doc-drift**: sweep README/CLAUDE.md/docs for the dissolved tool names.
10. Run `just run-bats-tag grit` until green.

## Risks / watch-items

- **arg-order drift**: grit.branch's ~12-subcommand union has a long arg-order;
  an off-by-one between arg-order and the dispatcher's `${N}` reads silently
  misroutes args. Cross-check positions explicitly.
- **Direct-$BIN bats callers** for rebase/restack/etc. may exist (like
  grit_push.bats did). Grep before assuming a clean migration.
- **diff caching**: diff.toml set `cache-results = "always"`; the family tools
  don't. Decide whether the diff subcommand should preserve that.
- **bin/diff arg mapping**: its positional order differs from how stage/branch
  expose flags; map carefully.
