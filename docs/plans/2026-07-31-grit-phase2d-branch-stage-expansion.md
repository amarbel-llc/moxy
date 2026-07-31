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

## The result-type conversion (discovered during build)

`bin/diff` emits a full MCP envelope (`{content:[{type:text,text:...,mimeType:...}]}`),
and `diff.toml` omits `result-type` — which for schema 2/3 **defaults to
`mcp-result`** (config.go `resolveResultType`), so diff is already an mcp-result
tool with `cache-results = "always"`.

But `grit.branch` and `grit.stage` are explicitly `result-type = "text"` (their
dispatchers print raw stdout). A tool has ONE result-type. Folding `diff` in —
which needs mcp-result to carry its mimeType and (per RFC 0001) a `_meta` cache
intent — therefore forces **the whole family to `result-type = "mcp-result"`**.
Every subcommand of both families must then emit an MCP envelope, not raw text.

### Shared emit helper

Add `bin/_emit` (a wrapper mirroring bin/diff's `jq -Rsc` envelope emission):
reads text on stdin, prints `{content:[{type:"text",text:.<,mimeType:$mime>}]}`.
Both `bin/branch` and `bin/stage` source/pipe through it so every branch's git
output becomes a valid envelope with one tested wrapper. The `diff` subcommand
still execs `@BIN@/diff` (already an envelope emitter with its own mimeType, and
the place a `_meta."moxy/cache":"always"` intent is added under the family's
`cache-results = "dynamic"`).

## Split diff

Keep `bin/diff` as an internal helper (no `diff.toml`). Both `bin/stage` and
`bin/branch` `diff` cases exec `@BIN@/diff` with the flags valid for that context:

- stage diff: `staged`, `paths`, `stat_only`, `context_lines` (no `ref`).
- branch diff: `ref`, `paths`, `stat_only`, `context_lines` (no `staged`).

`bin/diff` arg-order: `ref paths staged stat_only context_lines repo_path`. Each
family fills its exposed slots, leaves the rest empty. The families set
`cache-results = "dynamic"`; their `diff` subcommand emits
`_meta."moxy/cache":"always"` (preserving diff's current always-cache), while
other subcommands emit no intent (→ threshold fallback).

## Build steps — two stages

### Stage 1: convert grit.branch + grit.stage to mcp-result (no new folds)

De-risks by isolating the mechanical envelope conversion from the feature adds,
so the existing-subcommand regressions surface alone.

1. **bin/_emit**: shared helper — reads stdin, emits
   `{content:[{type:"text",text:.}]}` via `jq -Rsc` (optional `--arg mime`).
2. **bin/branch, bin/stage**: pipe every existing branch's git output through
   `@BIN@/_emit`. Branches that error (exit 2/1 with a stderr message) keep
   doing so — mcp-result mode only shapes stdout on success.
3. **branch.toml, stage.toml**: change `result-type = "text"` →
   `result-type = "mcp-result"` (or drop the line; schema-3 default is
   mcp-result).
4. Run `just run-bats-tag grit`. The existing grit_branch/grit_stage bats assert
   on `.content[0].text` etc. already (they use run_moxy_mcp_v1 / jq) — verify
   they still pass under the envelope. Fix any that asserted raw-string shape.
5. Commit Stage 1 ("refactor(grit): grit.branch/grit.stage to mcp-result").

### Stage 2: fold new tools + split diff

6. **bin/branch**: add `rebase`/`restack`/`cherry-pick`/`revert`/`log`/
   `rev-parse` cases (inline the old scripts, pipe through _emit) and a `diff`
   case that execs `@BIN@/diff` with the ref-form flag subset. rebase's
   continue/abort/skip become args; rev-parse drops the `git-` prefix.
7. **bin/stage**: add `status` case (inline, via _emit) and a `diff` case
   (exec `@BIN@/diff` with the index-form flags: staged/paths/stat_only/
   context_lines, no ref).
8. **branch.toml / stage.toml**: extend enum + union args + arg-order; set
   `cache-results = "dynamic"`. Both diff cases emit `_meta."moxy/cache":"always"`
   (via bin/diff, which already sets mimeType — add the intent there or wrap).
   arg-order MUST stay in sync with the dispatcher `${N}` positions.
9. **bin/branch-perms**: log/rev-parse/diff → allow (0); rebase/restack/
   cherry-pick → ask (1); **revert → allow (0)** (creates new commits, does not
   rewrite history — decided accurate over consistent). **bin/stage-perms**:
   status/diff → allow (0).
10. **Delete .toml**: diff, status, log, git-rev-parse, rebase, restack,
    cherry-pick, revert. **Keep bin/diff** (shared exec target). Delete the
    now-unused bin/ scripts whose logic was inlined (status/log/git-rev-parse/
    rebase/restack/cherry-pick/revert) — but ONLY after confirming no direct
    `$BIN/<tool>` bats caller needs them.
11. **Tests**: migrate grit_status/grit_diff/grit_log/grit_rebase/
    grit_cherry-pick/grit_restack/grit_revert bats to the new family+subcommand
    form. GREP FOR DIRECT `$BIN/<tool>` CALLERS (the grit_push.bats lesson) —
    not just `grit.<tool>` MCP names. Add stage status/diff + branch
    log/rev-parse/diff coverage. Verify split diff: stage diff has no `ref`,
    branch diff has no `staged`; and the diff subcommand still always-caches
    (madder:// URI) via the dynamic _meta intent.
12. **Doc-drift**: sweep README/CLAUDE.md/docs for the dissolved tool names.
13. Run `just run-bats-tag grit` until green. Commit Stage 2.

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
