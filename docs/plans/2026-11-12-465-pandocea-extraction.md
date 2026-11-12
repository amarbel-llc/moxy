# moxy#465: extracting piers into pandocea (needs an operator ruling)

Status: the preparatory step is done; the extraction itself waits on four
rulings. Nothing outward-facing (a new repository, moved issues) was done.

## Done: piers no longer carries its own gws runner

Before, `moxins/piers/src/gws.ts` held the gws runner (scratch cwd, one-line
errors) and `car`, `prison`, `gmail`, `calendar` and `gws.api` copied or lacked
it (#458, #467). All six now use `moxin-lib/gws-run.ts`; piers' `gws.ts` is a
thin layer over it (`PiersError extends GwsError`, JSON replies,
`docsBatchUpdate`, `printResult`). `GWS_BIN` stays the shared test seam.

What this means for the extraction: `moxin-lib/gws-run.ts` is outside piers,
which is where #467 said a shared runner should live, but a pandocea repo that
contains piers needs that one file too (see question 3).

## Questions

1. **Scope: the whole piers moxin, or only the markdown-to-Docs converter?**
   Recommended: **the whole moxin.** It is one product (a Docs toolset whose
   centrepiece is the converter), its tool files and `src/` entrypoints already
   import the converter directly, and splitting the converter from its
   callers first would be a second refactor with no consumer asking for it. The
   converter can become a library later, inside pandocea.

2. **How does moxy consume it afterwards?**
   Recommended: **a flake input whose moxin output moxy links into
   `gws-moxins` and `moxy-moxins-all`**, as moxy already does for madder and
   the other inputs. Piers is not shipped in the default bundle today, so the
   default `moxy-moxins` is unchanged. Serving it from pandocea entirely would
   need a second MCP server entry in every user's config for no gain.

3. **The shared gws pieces: duplicate or share?**
   Recommended: **duplicate `moxin-lib/gws-run.ts` into pandocea (about 60
   lines) and keep the pinned `gws` binary derivation in moxy for the other
   gws moxins, with pandocea pinning its own.** A common place both repos
   depend on costs a third repository for 60 lines and a binary pin. Re-check
   when a third consumer appears. The `run-gws` and `debug-gws-*` recipes stay
   in moxy (they serve car, gmail and the rest), and pandocea gets its own.

4. **The open piers issues here: transfer or keep?**
   Recommended: **transfer the issues that are only about piers** (#452, #453,
   #462, #463, #464) when the repository exists, and **keep** the ones about
   gws moxins generally (#331, #363, #445), with a link each way. Transferring
   is outward-facing, so it waits for the repository.

## Not started because

Questions 1 to 4 decide what leaves moxy, and creating `pandocea` (a new
repository) is the operator's call. Say yes to the recommendations, or change
them, and the next step is a spawn brief for the pandocea repo's session: that
session owns the converter's history, tests and CI, and the nix wiring in
moxy's `flake.nix` (`piers-moxin`, `piersDeps`, `piersFontsConf`,
`PIERS_DOCS_WRITER`) comes out in one moxy merge afterwards.
