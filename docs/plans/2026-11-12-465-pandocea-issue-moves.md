# moxy#465: issue-move texts (drafts, nothing filed)

For step 8 of `2026-11-12-465-pandocea-extraction.md`. Forgejo has no
cross-repository issue transfer that was verified, so each piers-only issue is
filed again in `linenisgreat/pandocea` and the moxy one is closed with a link
forward. Do this only after the pandocea repository exists and holds piers.

How: for each row, file the new issue with the title below and a body made of
the **forward header** followed by the original moxy body copied verbatim
(`smith issue view-body` on the moxy number); then comment the **closing
comment** on the moxy issue (with the new number filled in) and close it. In the
copied body, `moxins/piers/...` and `zz-pocs/pandoc-docs-poc` paths still hold:
the extraction keeps path names. Bare `#N` references in the copied body mean
moxy issues, so rewrite each to `linenisgreat/moxy#N`.

Forward header (new issue):

> Moved from linenisgreat/moxy#NNN when piers was extracted into pandocea
> (linenisgreat/moxy#465). The original text follows; `#N` references in it
> are moxy issues.

Closing comment (moxy issue):

> Moved to linenisgreat/pandocea#MMM: piers now lives in pandocea
> (linenisgreat/moxy#465). Closing this one; carry on there.

| moxy | title to file in pandocea | note |
|---|---|---|
| #452 | piers: examine hosting images on a madder media store on circus (krone) instead of transient Google Drive files | The body says research on the krone/madder side belongs to the circus session; keep that sentence. The `README` text in `moxins/gws/README.md` that points at `linenisgreat/moxy#452` moves to pandocea with the section and should point at the new number. |
| #453 | piers: markdown edge cases and untested paths left after image/PlantUML placement | Has one comment; copy it under the body as a quoted reply. Related list: #449 (moxy), #452, #445 (moxy, stays). |
| #462 | piers writer: merged table cells (row/col spans) | |
| #463 | piers writer: list structure edge cases (nested list in interior block, interior leading tab, mixed nesting kinds) | Related: #453 (moves too). |
| #464 | piers writer: carry footnote plans and pending images beside the requests, not inside them | |

Not moved: #331, #363 and #445 concern the gws moxins generally. Leave them in
moxy and comment on each with a link to pandocea, and on the matching pandocea
issue (where one exists) with a link back.

Sign-off on every filed text, as usual for this workspace: the Clown line with
the build identifier and commit link of whoever files it.
