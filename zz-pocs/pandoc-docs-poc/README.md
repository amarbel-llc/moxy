# pandoc-docs-poc

A pandoc custom Lua writer that turns markdown into Google Docs API
`batchUpdate` requests, so piers can write a Doc (or one tab of it) directly
instead of going through Drive's markdown import.

**Status:** POC — validated offline and live, then graduated: the shipped
writer is `moxins/piers/src/docs-requests.lua`, which has moved on since.
This copy is a frozen snapshot of the experiment; change the shipped one.
Tracking: #456 (update deletes other tabs), #457 (fenced code blocks),
#453 (markdown edge cases).

## Hypothesis

If pandoc parses the markdown and `docs-requests.lua` emits Docs requests,
piers can rewrite one tab's body in place — leaving other tabs and their
comments alone (#456) — with formatting Drive's importer does not give:
code blocks as shaded monospace paragraphs (#457), and a real parser instead
of piers' line-and-regex fence/image scanner (#453).

## Result

Validated. Against a scratch Doc, tab 1 was cleared and rewritten beside a
sentinel tab, which survived; the rewrite was checked through
`documents.get` and by eye in the Docs UI. Covered:

- headings, emphasis, strikethrough, links, inline code
- fenced code: one shaded Roboto Mono paragraph per fence, lines joined by
  soft breaks, indentation and tabs kept — including fences nested in list
  items and blockquotes, and a line starting with an inline ```` ``` ```` span
- bullet, ordered and task lists with nesting; an ordered list interrupted by
  a code block keeps one list id, so its numbering continues
- blockquotes (indent + left border), horizontal rules
- tables: header row bold and shaded, column alignment, rich cell content,
  no stray empty paragraph either side
- footnotes, with inline styles
- images: remote URLs directly; local files and ```` ```plantuml ```` blocks
  rendered and hosted transiently on Drive by the driver
- UTF-16 indexing (text after an emoji lands right)

Side findings worth keeping:

- Drive's markdown import turns each fenced-code line into its own unstyled
  paragraph with an empty paragraph between lines.
- pandoc expands tabs in code unless given `--preserve-tabs`.
- `updateParagraphStyle` with `fields: "*"` is refused inside a table cell
  (it includes `pageBreakBefore`).
- Capturing PlantUML's stdout through zx decodes it as UTF-8 and corrupts the
  PNG; `spawnSync` keeps a Buffer.
- Concurrent Drive permission grants were answered with a 500 once; hosting
  images one at a time has not hit it.
- A fresh footnote segment holds `" \n"`; content goes before the newline, so
  the note keeps Docs' leading space, as the UI does.

## Running

```sh
just explore-pandoc-docs-poc-offline  # no Google calls
just explore-pandoc-docs-poc-live     # scratch Doc in your Drive, deleted after
just debug-pandoc-ast <file.md>       # the AST the writer walks
```

The live driver needs a prior `just run-gws auth login`. Set `KEEP_DOC` in
`live.mjs` to keep the scratch Doc for a look in the UI.

## How the writer works

A rendered block list is a *plan*: requests that write it into an empty
segment whose insertion point is index 1. Inside a plan:

1. one `insertText` of all the text, then resets of inherited style (the
   cleared tab's last paragraph would otherwise lend the text its style)
2. paragraph and text styles (adjacent runs styled alike share a request)
3. bullets, last group first — `createParagraphBullets` deletes the leading
   tabs it reads nesting from, shifting later indexes
4. structural ops — tables, images, footnote references — in descending
   position, mapped through step 3's tab removals, so each leaves the
   positions of those still to come intact

Table cells and footnotes are nested plans, placed by shifting their
indexes. A table is anchored at the newline ending the previous paragraph;
Docs inserts a newline before the table, so it starts at `q+1`, and cell
(r, c) of an empty R×C table holds its content at `q+4 + r(1+2C) + 2c`.
Footnote content goes in a second `batchUpdate`, once `createFootnote`'s
reply has given the footnote ids.

The writer leaves two things to its caller: an `insertInlineImage` with a
`pending` field (a local path or PlantUML source) needs its `uri`, and each
entry of `footnotes` is applied at its footnote's segment.

## Out of scope / open

- Comments anchored on rewritten text are lost (true of any rewrite).
- Merged table cells (row/col span).
- A list nested inside a non-list block within a list item starts its own
  list; an interior paragraph of a list loses any leading tab of its own.
- The Docs API cannot set a checkbox's checked state; done task items are
  struck through instead.
- Image display size is left to Docs; piers' sizing (natural size at 2x
  render, fit to page) would carry over.
- Alt text: `insertInlineImage` has no field for it.

## Notes for graduation

1. Ship `docs-requests.lua` with the piers moxin (pandoc on the wrapper's
   PATH), and replace the Drive import in `create`/`update` with: create an
   empty Doc / clear the target tab, apply the plan, then footnotes.
2. Give `update` a `tab_id` (default: the first tab).
3. Reuse `inline-images.ts`' hosting, sizing and cleanup for `pending`
   images; its markdown scanner goes away.
4. Port `offline.mjs`' checks to bats against the gws stub; the stub's
   `documents get` must answer with real structure for footnotes.
