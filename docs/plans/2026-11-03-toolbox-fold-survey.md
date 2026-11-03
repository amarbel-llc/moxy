---
status: proposed
date: 2026-11-03
issue: moxy#502
related: FDR 0016 (toolbox), moxy#496 (permissions matrix), moxy#501
  (manpages as resources), moxy#490 (read-path policy), moxy#495 (jq/rg
  input hardening), moxy#482 (grit leaves moxy), spinclass
  docs/plans/2026-11-03-structural-confined-moxy.md
---

# Folding read-only moxin operations into toolbox: a survey

Survey only. Nothing here is built, and the default permission of every tool
and the contents of every profile are the operator's decision; this note
proposes. Labels: **V** = verified this session (measured, or read in code),
**I** = inferred, not checked. Peer input is attributed by session id.

## 1. The question

Every moxin tool puts its name, description and input schema into each
session's context, whether or not the session calls it. toolbox (FDR 0016)
is one tool whose description stays short and defers usage to manpages
(toolbox(1), toolbox-<applet>(1)). Which existing operations could become
toolbox applets, what would that save, and what has to stay a tool of its own?

## 2. Method: what a moxin costs a session

`just debug-tool-catalog-size [server]` (added with this note) serves moxy
over every built moxin (`moxy-moxins-all`), takes the `tools/list` result,
and sums the compact-JSON byte size of each tool entry (name, description,
input schema, annotations, `_meta`) per server.

- **V**: bytes are what moxy sends. **I**: tokens ≈ bytes / 4 is a rough rule
  for English-and-JSON text; clients render schemas their own way, and a
  client that defers tool schemas (loads them on demand) pays only for names
  and short descriptions, so savings there are smaller than below.
- Out of reach of this recipe: moxins shipped by other repos (`smith`,
  `chrest`, `nebulous`, `stats-me`, `issue-filer`) and non-moxy servers.
- The 157-tool catalog figure clown/rare-walnut cited is **not verified**
  here. For comparison (**V**, from this session's own server list, a
  pre-toolbox build): 158 moxy tools across 19 servers. 68 of them are from
  servers outside this repo (smith 34, chrest 19, nebulous 9, stats-me 5,
  issue-filer 1). The other 90, plus arboretum (7, not in the default bundle),
  the 6 meta tools and toolbox (1), give the 104 measured below.

Measured (**V**, 2026-11-03, tree at 66b98f4 plus this note):

| server | tools | bytes | largest tool |
|---|--:|--:|---|
| grit | 9 | 18,566 | grit.branch 4,932 |
| piers | 7 | 13,176 | piers.update 3,354 |
| chix | 22 | 12,976 | chix.why-depends 1,464 |
| folio | 16 | 11,980 | folio.tree 1,298 |
| freud | 12 | 11,025 | freud.tool-results-summary 1,463 |
| arboretum | 7 | 5,814 | arboretum.search 1,366 |
| env | 9 | 4,494 | env.date 1,100 |
| car | 5 | 3,123 | car.export 813 |
| rg | 1 | 2,428 | rg.search |
| man | 3 | 1,996 | man.section 922 |
| jq | 1 | 989 | jq.jq |
| toolbox | 1 | 798 | toolbox.run |
| gmail, prison, calendar, gws | 5 | 2,558 | |
| meta (async ×3, batch, restart, status) | 6 | 4,659 | |
| **total** | **104** | **94,582** | |

Per-tool figures for the candidates below come from the same recipe with a
server argument.

## 3. What makes an operation foldable

FDR 0016's admissibility rules, unchanged: **pure** (output is a function of
input bytes and flags), **no side effects**, **no exec**, **typed flags only**.
Two consequences matter for this survey:

- File inputs must be *file operands* the grammar can find, so the policy
  broker judges each one (`[resources.command] grammar = "toolbox"`) and
  `toolbox run` opens exactly the judged path without following symlinks.
- Today that open path takes **regular files only** (**V**: `run.go` refuses
  non-regular files). Anything that walks a directory needs more (section 5).

## 4. Per moxin

### jq: foldable (high value, small cost)

- **V**: already pure-shaped after moxy#495: flags are an allowlist, the ones
  that read files (`-f`, `--rawfile`, `--slurpfile`, `--argfile`, `-L`) are
  refused, the filter runs with an empty environment from a program file, and
  inputs are `files` (judged as `read`) or `stdin`.
- As an applet: `jq -r '.items[] | .name' data.json`. The filter is a single
  quoted word; toolbox's grammar keeps `|`, `$`, `[]`, `{}` and `?` literal
  inside single quotes (**V**: lex rules), so a filter needs no escaping
  beyond sh's.
- To check before building (**I**): jq's `now`, `localtime` and
  `strflocaltime` read the clock or the timezone, which breaks rule 1. Either
  accept that as a documented exception, or reject them at parse time (which
  needs jq's own parser). `import`/`include` with the default search path
  could read module files outside the policy; it should be confirmed that
  `env -i` plus no `-L` leaves only read-only store paths, or modules should
  be refused.
- Result shaping: jq uses `cache-results = "always"` so results chain. toolbox
  caches at the default threshold (**V**: no `cache-results` key). Folding jq
  in would need either always-cache for toolbox, or a per-pipeline choice.
- Saves (**V** bytes): 989 bytes ≈ 250 tokens (**I**).

### rg: foldable, on one condition

- **V**: rg.search declares `path:read` on a directory root, and only the root
  gets a verdict (moxy/sunny-sumac confirmed this from its generated matrix);
  the folio-perms floor is the same.
- The condition, agreed with moxy/sunny-sumac: an rg applet must walk the
  tree **fd-relative from the judged root**, `openat2` with
  `RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS`, so the judged root is exactly the
  tree that is read. Passing rg a `/dev/fd/N` directory and letting it open
  children by path would pin the root but leave a swap race below it (**I**).
  That means either an in-process search (Go regexp plus an ignore-file walker)
  or a patched rg that takes a directory fd. Either is real work. It is the
  reason to do rg after jq.
- Flags to leave out of the table (**I**, from rg's documentation, not
  re-read here): `--pre` and `--pre-glob` (exec), `-z`/`--search-zip` (runs
  decompressors), `--ignore-file`, `-f`/`--file` (read unjudged files),
  `-L`/`--follow`.
- Saves (**V** bytes): 2,428 ≈ 600 tokens (**I**). Most of that is its ten
  parameters documented in the schema; as an applet they move to
  toolbox-rg(1).

### folio, read side: partly

| tool | bytes (**V**) | verdict |
|---|--:|---|
| read | 1,282 | **fold.** Line numbers and ranges are a pure `lines` applet (or `nl`, `head`, `tail`) over one judged file. |
| tar (list) | 808 | **fold.** `tar -t` on a judged archive file, with only the list flags in the table (**I**, moxy/sunny-sumac agrees). |
| file-type | 493 | **fold, with care.** `file(1)` is pure but runs libmagic over untrusted bytes; worth a sandboxing look (**I**). |
| ls, tree, glob, du | 3,482 | **fold only after rg's walk exists.** They reveal names below the root on a root-only verdict (**V**, moxy/sunny-sumac); the same fd-relative walk applies. Whether a directory verdict covers its contents is the broker stanza's call (spinclass/light-cedar's lane, not verified). |
| df | 558 | **leave.** No stanza, no gate, opens no file (**V**, moxy/sunny-sumac); it reports host state, not a function of input. |
| write, cp, mv, rm, mkdir, mktemp, chmod, link | 5,357 | **stay.** They have side effects. |

Saves if read, tar and file-type fold: 2,583 bytes ≈ 650 tokens (**I**);
plus 3,482 ≈ 870 tokens once directory walking exists.

### arboretum: read side foldable, rewrite stays

- outline, md-toc, md-section, md-anchor and search (4,234 bytes, **V**) are
  read-only parses of a file or stdin (**I**, from their descriptions and
  smoke recipes). They are applet-shaped once their file inputs are operands.
  search walks a directory, so it waits for the same walk as rg.
- rewrite writes files: stays. abi-check is a developer check: drop from any
  session catalog rather than fold.
- arboretum is not in the default bundle today (**V**: README), so this saves
  nothing for most sessions until it is.

### env: not foldable under FDR 0016's rules

- clock, uptime, loadavg, meminfo and cpu read host state; which and get read
  the environment (moxy#497 is restricting get to an allowlist); resolve reads
  link targets. None of them is a function of its input (rule 1). (**V**: tool
  TOMLs.)
- `date` with an explicit epoch and an explicit timezone is pure, but it
  accepts relative strings (`now`, `yesterday`) and defaults to the host zone.
  A restricted `date` applet is possible; it is a small saving (1,100 bytes).
- To fold env at all would mean a second applet class ("observes the host,
  takes no resource") with its own rules. That is a change to toolbox's
  contract and an operator decision, not a survey finding.
- Cost to keep (**V**): 4,494 bytes over 9 tools, the largest part of the
  proposed confined default (section 6).

### man: replace with resources (moxy#501), do not fold

- man's inputs are page names, not files, so the policy broker has nothing to
  judge; it is always-allow (**V**: TOMLs). An applet would only rename it.
- Serving the pages as read-only MCP resources (moxy#501), starting with
  toolbox's own pages, removes the tool from the catalog altogether (1,996
  bytes, **V**) and keeps disclosure incremental: a page costs context only
  when it is read.

### freud: stay separate

- Its inputs are session ids, not paths, and it reads other sessions'
  transcripts: in #496's review, the largest remaining information exposure
  (**I** there, quoted). Folding it into toolbox would make that exposure part
  of every profile that has toolbox. It should first become resource-judged
  (a `session:` kind in the broker, say), and that is #496's call.
- Size: 11,025 bytes over 12 tools (**V**).

### chix: store readers duplicate, the rest stays

- store-cat and store-ls read world-readable `/nix/store` paths, which folio's
  floor already allows (**V**: folio description). They are covered by
  `folio.read`/`ls` today and by applets later; dropping them from a catalog
  saves 865 bytes (**V**).
- The other 20 tools talk to the nix daemon, evaluate (which may fetch) or
  build: side effects and network. Stay.

### Stays separate regardless

grit (writes; leaving moxy, moxy#482), piers, car, gmail, calendar, prison,
gws, slip (credentials and network), the meta tools, and the out-of-repo
moxins (smith, chrest, nebulous, stats-me, issue-filer).

## 5. What toolbox needs first

1. **Per-applet exposure.** `allow-tools` works on MCP tools (**V**:
   moxyfile(5)), and toolbox is one tool, so a profile that keeps
   `toolbox.run` gets every applet. Folding rg in would grant rg to every such
   profile. Before any applet with a different risk lands, toolbox needs its
   own applet allowlist that the moxyfile or profile can set and narrowing
   (FDR 0013) checks. Proposal (**I**): a `[servers.toolbox] applets = [...]`
   key, narrowing as an intersection, default = round-one set.
2. **The fd-relative walk** (section 4, rg) before any directory applet.
3. **Result shaping per pipeline**, at least always-cache, if jq comes in.
4. **#496 rows.** moxy/sunny-sumac: the matrix unit stays the MCP tool; the
   audit doc will add a sub-table under `toolbox.run` with one row per applet,
   labelled "inherits tool tier and exposure", carrying only the op that
   applet takes. The generator cannot produce those rows (the applet's op is
   not in the TOML), so the source for them is toolbox's applet table, and
   later this survey. RFC 0002 has only read, write and exec; a distinct
   `list` op for directory listings would be an RFC change, worth asking for
   only if the operator wants "see names" separated from "read contents".

## 6. The confined default: toolbox + env + man

The operator's default set (moxy#502 comment, 2026-11-03), measured (**V**):

| set | tools | bytes | ≈ tokens (**I**) |
|---|--:|--:|--:|
| toolbox + env + man, today | 13 | 7,288 | 1,800 |
| + rg + jq as separate tools | 15 | 10,705 | 2,700 |
| rg and jq as applets (toolbox grows by the applet names, ~+50 bytes, **I**) | 13 | ~7,340 | 1,850 |
| man moved to resources (#501) | 10 | ~5,340 | 1,350 |
| env as a host-observation applet class too (operator decision) | 1 | ~1,000–1,500 | 300 |

The last row is the "confined default stays one tool" case the operator's note
asks about. It costs what toolbox's own description costs, plus whatever
those applet names add, and everything else is read from manpages when used.
It needs all three of: section 5's applet allowlist, #501, and a ruling on
env's applet class.

Relative to the whole measured catalog (94,582 bytes), the foldable candidates
(jq, rg, folio's read side, arboretum's read side) are 14,274 bytes ≈ 3,600
tokens (**I**); with env and man, 20,764 ≈ 5,200. The biggest costs in the
catalog are tools that should not fold (grit, piers, chix, freud), so for an
unconfined session the larger saving is in *which moxins a profile serves*,
not in folding.

## 7. Incremental disclosure

- toolbox.run's description lists applet names and points to toolbox(1)
  (**V**). Each applet has toolbox-<applet>(1), and a test fails if any applet
  lacks one (**V**: `zz-tests_bats/toolbox.bats`).
- A flag error names the page: `unknown flag -x (see toolbox-sort(1))` (**V**:
  `pkg/toolbox/parse.go`). So a wrong guess costs one round trip and points at
  the answer.
- Today the pages are read with `man.toc`/`man.section`, so they need the man
  moxin. With #501 a session that has only toolbox can read them as resources.
- For rg and jq, agents mostly know the CLI already (**I**); an applet that
  keeps the familiar spelling costs nearly nothing to learn. Disclosure matters
  most for refusals: the page should say which flags are absent and why.

## 8. Proposed order

1. jq as an applet, with the clock/module questions settled and always-cache.
2. The applet allowlist (section 5.1), before anything with a different risk.
3. folio read, tar list and file-type as applets.
4. The fd-relative walk, then rg, ls/tree/glob/du and arboretum search.
5. Retire the standalone tools from the default profiles once their applets
   exist (each one is a profile change: operator).

Each step is its own issue and merge. Nothing here changes the path engine
(moxy/sunny-sumac's lane) or any profile (clown's lane, the operator's
decision).
