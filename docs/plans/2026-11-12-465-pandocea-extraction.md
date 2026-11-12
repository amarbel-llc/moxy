# moxy#465: extracting piers into pandocea

Status: prepared, 2026-11-12. Everything local and reversible is done and
rehearsed in a scratch clone. **Nothing has been created or pushed on the
forge**: the repository name and the issue transfer are for the coordinator to
confirm first, and the steps that write to the forge are marked below.

The operator said to proceed. The recommendations of the earlier version of
this note (whole moxin; moxy consumes a flake input; duplicate the small gws
runner; transfer only piers-only issues) are taken as the plan; the repository
name and the transfer wait for confirmation.

## Done in this repo

- `piers`' `gws.ts` is a thin layer over `moxin-lib/gws-run.ts`, the runner
  shared with car, prison, gmail, calendar and gws.api, so the runner exists
  once here (before: piers had its own copy).
- `just explore-pandocea-extract` rehearses the history-preserving extraction:
  it clones this checkout to `.tmp/pandocea-extract` and rewrites that clone
  with `git filter-repo` (from nixpkgs) so only these paths remain, with their
  history and unchanged names:
  `moxins/piers`, `zz-pocs/pandoc-docs-poc`, `zz-tests_bats/piers_tools.bats`,
  `piers_markdown.bats`, `piers_images.bats`, `piers_stub.bash`,
  `moxin-lib/gws-run.ts`, `package.json`, `bun.lock`, `bun.nix`.
  Result on the rehearsal: **33 commits, 34 files** (21 under `moxins/piers`),
  oldest 2026-04-12 (the root `package.json`), piers' own history from
  2026-04-17 ("split gws moxin into product-specific moxins").
- `just explore-pandocea-skeleton` lays the skeleton in
  `docs/plans/2026-11-12-465-pandocea-skeleton/*.draft` over that clone
  (`flake.nix`, `justfile`, `AGENTS.md`, `version.env`, a `.gitignore`) and
  builds `.#piers` there. **It builds**: the result has all eight tools, and no
  `@BIN@`, `@FOLIO_PERMS@` or `@PIERS_DOCS_WRITER@` placeholder is left.
  That is the extent of the check; see "Not verified".
- **The bats lane is ported** (step 6, done in the rehearsal): the skeleton adds
  a `bats` input, `checks.bats-piers` (a `batsLane` over `PIERS_BIN`) and
  `zz-tests_bats/common.bash.draft` (five `bats_load_library` lines; the piers
  tests need no `setup_test_home`). `just explore-pandocea-skeleton` now also
  builds that check; **all 62 piers tests passed** in the scratch extraction,
  including the `piers.rename` ones.
- **The moxy side is prepared, unmerged** (step 7): `just
  explore-pandocea-moxy-clone` makes a scratch clone on a local branch
  `pandocea-input`; the edits listed in step 7 were made there by hand, and `just
  explore-pandocea-moxy-check` builds `.#gws-moxins` in it with `--override-input
  pandocea git+file://…/.tmp/pandocea-extract` and writes the staged diff to
  `.tmp/moxy-pandocea.patch`. **That build succeeded**: the result has
  calendar, car, gmail, gws, piers and prison, and piers has all eight tools.
  The clone is not a branch of this checkout and was never pushed. The patch is
  under `.tmp`, so it goes away with the worktree; re-making it is the clone
  recipe plus the step 7 edits. What the check did **not** run: moxy's own gate
  (`nix flake check`) and its bats lanes on the clone; `flake.lock` has no
  `pandocea` node yet, so a plain `nix build` there fails until step 4 and
  `nix flake lock --update-input pandocea`.
- Issue-move texts for #452, #453, #462, #463 and #464 are drafted in
  `2026-11-12-465-pandocea-issue-moves.md`, not filed.

## Layout kept

Paths keep their names (no rename to a root-level moxin). That keeps
`git log --follow` simple and lets `mkBunMoxin`'s `./moxins/<name>/src`
fileset work unchanged. A later tidy can rename.

## How moxy consumes it

pandocea does **not** input moxy; moxy inputs pandocea. The reason is
`@FOLIO_PERMS@`: piers' `create.toml` and `update.toml` point their
dynamic-perms at moxy's folio-perms script (the read policy for
`content_path` and `base_dir`). If pandocea built the moxin from moxy's script
the two flakes would depend on each other. Instead pandocea exports a function:

```
pandocea.lib.<system>.mkPiers folioPermsScript   # -> the piers moxin
```

and its own `packages.piers` passes a stand-in that allows everything, for
standalone builds only. In moxy's flake the change is:

```nix
inputs.pandocea = {
  url = "https://code.linenisgreat.com/pandocea/archive/master.tar.gz";  # location to confirm
  inputs.igloo.follows = "igloo";
  inputs.nixpkgs-master.follows = "nixpkgs-master";
  inputs.utils.follows = "utils";
};
# ...
piers-moxin = pandocea.lib.${system}.mkPiers "${folio-moxin}/bin/folio-perms";
```

`piers` stays in `gwsMoxinNames`, `allMoxins` and `unshippedMoxins`, so the
bundles (`gws-moxins`, `moxy-moxins-all`) are unchanged for a user.

`mkBunMoxin` is copied into pandocea (about 110 lines). moxy#302 and #434 ask
for it as a moxy export; pandocea could take that later, but a moxy input in
pandocea would close the cycle above, so the copy is the plan until it matters.

## Ordered steps

Marked **[forge]** are outward-facing and need the coordinator's go. Before
step 2 everything is local.

1. **Confirm** the repository name (`pandocea`) and where it lives (owner and
   host; moxy's other inputs are `code.linenisgreat.com/<repo>`).
2. **[forge]** Create the empty repository.
3. Freeze piers changes in moxy for the length of the move (no merge touching
   `moxins/piers` or `zz-tests_bats/piers_*` between 3 and 7), then re-run
   `just explore-pandocea-extract` on current master so the history is final.
4. **[forge]** From the scratch clone: `just explore-pandocea-skeleton` (lays the
   skeleton), commit it (signed, so piggy-agent must be unlocked), add the
   remote, push `master`, commit `flake.lock`.
5. Build `.#piers` from the pushed flake ref (not the scratch clone) on a clean
   machine; that is the first test of the skeleton outside the scratch clone.
6. The bats lane is part of the skeleton (see "Done in this repo"); check
   `nix build .#checks.<system>.bats-piers` from the pushed flake ref.
7. One moxy merge (prepared in the scratch clone, see above): add the `pandocea` input; `piers-moxin` from `mkPiers`;
   delete `moxins/piers`, `zz-tests_bats/piers_tools.bats`, `piers_markdown.bats`,
   `piers_images.bats`, `zz-pocs/pandoc-docs-poc`, the piers-only recipes in the
   justfile (`explore-pandoc-docs-poc-*`, `debug-piers-request-size`,
   `debug-write-scratch-png`, `debug-zx-arg-limit-repro`), `piersDeps` and
   `piersFontsConf` in `flake.nix`, and `PIERS_BIN` in the bats lane. **Keep
   `zz-tests_bats/piers_stub.bash`**: `gws_api.bats` and `gws_moxins.bats` use
   it. Regenerate `docs/permissions-matrix.md` (`just debug-perms-matrix`;
   piers rows go if the matrix is built from `moxins/`), move the "Markdown in
   piers" section of `moxins/gws/README.md` to pandocea, and run the gate.
8. **[forge]** Move the piers-only issues #452, #453, #462, #463, #464: Forgejo
   has no cross-repository issue transfer that I verified, so the plan is to
   file each in pandocea with a link back and close the moxy one with a link
   forward. Keep #331, #363 and #445 (gws moxins generally) with a link each
   way. The texts are in `2026-11-12-465-pandocea-issue-moves.md`.
9. **[forge]** Comment on and close #465 with the new repository's address.

### The push, in order

`<forge>` is the address the coordinator creates (private, empty, under
`linenisgreat`); `<ssh-url>` is that repository's ssh clone URL. piggy-agent
must be unlocked for the signed commit and the push. Run from this checkout:

```
just explore-pandocea-skeleton          # fresh extraction + skeleton + build + bats lane
cd .tmp/pandocea-extract
git -c commit.gpgsign=true commit -m "pandocea: flake, justfile, AGENTS.md, bats lane (moxy#465)"
git remote add origin <ssh-url>
git push origin HEAD:master
nix flake lock && git add flake.lock && git commit -m "flake.lock" && git push origin HEAD:master
nix build "git+<ssh-url>?ref=master#piers"   # first build from the pushed ref
```

then in the moxy checkout, with piers frozen (step 3) for the whole sequence:
re-make the moxy-side clone (`just explore-pandocea-moxy-clone`, redo the edits
or apply the saved patch), `nix flake lock --update-input pandocea` there, run
moxy's gate, and merge once. Then step 8 and 9 on the forge. The input URL form
for a private repository (an archive tarball needs auth the fetch does not have;
`git+ssh://` may be the working form) is **not verified**: confirm it at the
`nix flake lock` step above before the moxy merge.

Reverting: before step 2 delete `.tmp/pandocea-extract`. After step 2 delete the
repository; the moxy change in step 7 is one revertable merge.

## Not verified

- The skeleton was built only in the scratch clone, only for the current
  system: `.#piers` and the `bats-piers` check. `.#gws` and the devshell were
  not built; no other system was tried.
- Nothing ran piers against Google (no live call); the bats suite runs against
  a stub `gws`.
- The pandocea input URL for a private repository (see "The push").
- Issue transfer on this forge was not tried (step 8).
- `bun.nix` and `bun.lock` are moxy-wide (they carry `web-tree-sitter` for
  arboretum); pandocea only needs `zx`, so a trimmed `bun2nix` regeneration is
  worth doing after the move. Their carried history is moxy-wide too.
- piers' history before 2026-04-17 lives in moxy under `moxins/gws` and is not
  carried; `--path` follows exact paths only.
