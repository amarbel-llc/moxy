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
6. In pandocea, port the bats lane: the subset of moxy's
   `zz-tests_bats/common.bash` the piers tests use (`setup_test_home`, the
   JSON-RPC helpers), the `bats` libs input, and a `batsLane`. Until this is
   done pandocea has no tests that run (see the skeleton's `justfile.draft`).
7. One moxy merge: add the `pandocea` input; `piers-moxin` from `mkPiers`;
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
   way.
9. **[forge]** Comment on and close #465 with the new repository's address.

Reverting: before step 2 delete `.tmp/pandocea-extract`. After step 2 delete the
repository; the moxy change in step 7 is one revertable merge.

## Not verified

- The skeleton was built only in the scratch clone, only for the current
  system, and only `.#piers`. `.#gws` and the devshell were not built; no other
  system was tried.
- Nothing ran piers against Google (no live call), and the extracted bats tests
  do not run outside moxy yet (step 6).
- Issue transfer on this forge was not tried (step 8).
- `bun.nix` and `bun.lock` are moxy-wide (they carry `web-tree-sitter` for
  arboretum); pandocea only needs `zx`, so a trimmed `bun2nix` regeneration is
  worth doing after the move. Their carried history is moxy-wide too.
- piers' history before 2026-04-17 lives in moxy under `moxins/gws` and is not
  carried; `--path` follows exact paths only.
