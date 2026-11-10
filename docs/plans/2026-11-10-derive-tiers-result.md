# Derived tier and kind vs declared tier: measurement result

Status: result note, 2026-11-10, moxy/live-aspen/koko. Read-only; nothing in
the moxins changed. Follows
`2026-11-10-permissions-unification-research.md` (d)3.

Reproduce: `just debug-derive-tiers` (runs `tools/derive-tiers.awk` over the
committed `docs/permissions-matrix.md`). Labels: **[observed]** is output of
that recipe this session; **[inferred]** is my reading of it.

## Rule used (a first guess, not a proposal)

- kind: `read-only=true` in the hints is `read`; anything else is `write`.
  `exec` is not derivable (no hint says it).
- tier: any `[resources]` stanza is `dynamic`; read with none is
  `always-allow`; write with none is `delegate-to-client`.

## Result [observed]

104 tools. Derived kind: 72 read, 32 write. **84 agree, 20 differ.**

| declared -> derived | tools |
|---|---|
| always-allow -> always-allow | 58 |
| dynamic -> dynamic | 24 |
| delegate-to-client -> delegate-to-client | 2 |
| always-allow -> delegate-to-client | 13 |
| dynamic -> delegate-to-client | 7 |

The 20 differences fall in two groups, and neither is a flaw in the rule so much
as a place where the declared tier and the hints say different things:

1. **13 tools that are `always-allow` but not read-only by their hints**:
   `chix.build`, `chix.flake-init`, `chix.flake-lock`, `chix.flake-update`,
   `chix.run`, `chix.store-realize`, `folio.mktemp`, `grit.commit`,
   `grit.merge`, `gws.api`, `piers.batch-update`, `piers.comment`,
   `piers.tab`. Under the derivation each would prompt (or be refused when
   nobody can answer); today none does. [inferred: several are the tools an
   unattended worker needs routinely (`grit.commit`, `chix.build`), so the
   derived default would make them ask, and a blessed profile with `perm=allow`
   (option B) is exactly what would put them back. That is the case for B for
   `ask`.]
2. **7 `dynamic` tools with no `[resources]` stanza**: `grit.branch`,
   `grit.clone`, `grit.remote`, `grit.stage`, `grit.stash`, `grit.tag`,
   `grit.worktree`. They are `dynamic` through a code predicate (`branch-perms`,
   `stage-perms`, ...) rather than through declared resources, so the rule
   cannot see the path or ref logic. [inferred: this is the "tier stays a floor
   for dynamic" half of the ruling; deriving these away would drop the
   protected-branch rules.]

## Caveats [inferred]

- "Not read-only by hint" includes tools with no read-only hint at all (for
  example `folio.mktemp` has only `idempotent=false`), so `write` here means
  "not declared read-only", which is weaker than "writes". An author-declared
  kind (the ruling's "author overrides only where the hints cannot say") would
  fix these one by one.
- `exec` has no hint, so `chix.run`, `chix.build` and `gws.api` land in
  `write`; they would need an author override to be `exec`.
- The matrix covers moxin tools only (104); child-server tools (smith, chrest,
  nebulous, ...) have no tier or hints here and are not measured.
- 58 + 24 + 2 = 84 agreements are partly by construction for the read-only
  `always-allow` tools; the informative numbers are the 20 differences.
