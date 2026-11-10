# moxy#489 shadow mode: field-typing table for the profile tools

Status: design note, 2026-11-10. No code. Written ahead of cutting-garden #346
(`pkgs/ajour` `Evaluate`); the `Call` builder in `pkg/moxy` is not started.

## What this is

moxy#489 shadow mode (ajour phase 4) evaluates each call against a capability
profile, logs the verdict and enforces nothing. To do that `Call` must present a
tool call to `Evaluate` as typed nodes. This note lists the moxy tools the D2
sample profiles use (circus `docs/plans/2026-10-22-d2-profile-schema.md`
sections 3 and 4) and how each field of each is typed.

The typing rules are NOT decided here. They are clown RFC 0018
(`docs/rfcs/0018-capability-catalog-contract.md`, commit `d4df5b6` on clown
branch `ready-elder`, not yet on master as of this writing) and its data file
`internal/0/catalogcontract/catalog.json`, read through `pkgs/catalogcontract`
(`Load`, `Field(plugin, server, tool, field)`, `SchemeOwner(scheme)`, `Raw()`).
Per the operator ruling relayed 2026-11-10, moxy's table is a copy of that file,
not a second source. The rows below are what I expect that file to say for these
tools, read from the moxin manifests in this worktree. Every row is to be
checked against `catalog.json` when it lands; where they disagree, the file wins.

## Typing rules in play (from the RFC 0018 summary, unverified against the RFC text)

- Path of a call: `[moxy, <child server>, <tool as the manifest spells it>]`.
- Facts of the call: `kind`, `perm` (always-allow = allow, each-use or
  delegate = ask, dynamic = dynamic), `async`, `impure`.
- A path input becomes `fs-path-v1` `file:<home-relative or /absolute canonical>`.
- A non-`file:` URI in a path field is a resource read under the scheme
  owner's surface (a `madder://` blob is moxy's: `moxy/moxy`).
- A reference field becomes a resource path; `jq.stdin` that is a whole
  `madder://blobs` URI is a resource read, else tool-input.
- Everything else is `tool-input-v1 {id, field, value}`.
- An omitted env-resolved field is presented resolved; refuse if it cannot be.

## The table

Source column: the manifest key in `moxins/<server>/<tool>.toml` that already
says something about the field. `substitute-args` and `[resources.<field>]` are
the two keys that overlap the typing.

| Server.tool | Field | Expected type | Manifest evidence |
|---|---|---|---|
| rg.search | path | fs-path-v1 (`op=read`); defaults to CWD when omitted, so present it resolved | `substitute-args = ["path"]`, `[resources.path] op = "read"` |
| rg.search | pattern, glob, type, output_mode, context, case_insensitive, multiline, hidden, max_columns | tool-input-v1 | none |
| folio.read | file_path | fs-path-v1 (`op=read`); a `madder://blobs/<digest>` value is a resource read under moxy/moxy | `substitute-args = ["file_path"]`, `[resources.file_path] op = "read"` |
| folio.read | start, end, delete_start, delete_end | tool-input-v1 | none |
| folio.glob | path | fs-path-v1 (`op=read`); defaults to CWD, present resolved | `[resources.path] op = "read"` |
| folio.glob | pattern | tool-input-v1. Note: a pattern like `../x/*` or an absolute one reaches outside `path`; shadow mode should log it, and whether it is a path field is a RFC 0018 question | none |
| man.list | none | no fields | `always-allow`, `permit-async = false` |
| man.toc | page | tool-input-v1 | none |
| man.section | page, section_name | tool-input-v1 | none |
| smith.issue-get, smith.pr-get | repo, number, host, comments, view, output_format | tool-input-v1. `repo` and `host` are env/cwd-resolved when omitted; present resolved or refuse | smith is not declared under `moxins/` in this worktree, so I found no manifest to read; unverified |
| async | tool, args, timeout | present the wrapped call (tool + args) as its own Call, not as tool-input | meta tool, `internal/asyncjob` |
| batch | calls | present each wrapped call | meta tool, `pkg/perms` |

Tools in the D2 samples that are NOT moxy's and so are out of this table:
`run_recipe`/`list_recipes`/`show_recipe` (just-us plugin), `grit_stage` and
`grit_branch` as spinclass's own grit (the moxy `grit` server has the same
tool names but a different plugin path), troupe, ringmaster, cutting-garden,
attest.

## Gaps I can see

1. **The sample profiles name `grit_stage` and `grit_branch` under spinclass,
   not under moxy.** moxy also serves `grit.stage` / `grit.branch` with a
   `subcommand` field that the profile types as a `tool-arg-v1`. Whether
   catalog.json lists moxy's grit and how `subcommand` maps is not in what I
   have read.
2. **Defaults.** `rg.search.path` and `folio.glob.path` default to CWD inside the
   moxin script. The RFC says present env-resolved fields resolved; the CWD is
   known to moxy at dispatch, but the `Call` builder needs it passed in.
3. **`folio.glob.pattern` can leave `path`.** Not a typing question I can settle.
4. **`man://` resources** (moxy#501) are a resource surface owned by moxy; the
   profile samples do not cover them yet.
5. **smith manifests**: I did not locate the smith tool manifests; find them
   before filling the smith rows. The smith moxin is another session's lane.

## RFC 0018 section 9.4: a manifest key instead of the table

The overlap in the Source column suggests moxy could declare field typing in
the manifest instead of keeping a table: `[resources.<field>]` already marks
path fields with an `op`, and `substitute-args` already marks fields moxy
rewrites. What is missing is a way to say "this field is a reference" and
"this field is a content blob" (`jq.stdin`), and a notion of default-resolved.
That would make the manifest the source and `catalog.json` a generated view.

This is a design choice with a cost: it moves typing authority from clown's
file into moxin manifests, against the ruling that moxy copies the file. I am
not proposing it as the plan, only recording that the choice exists. It needs
an operator or coordinator ruling before anyone builds it.

## Checked against catalog.json (clown master 59954b9411d4)

The file landed after the table above was drafted; I read it. It agrees with
every row, and settles several gaps:

- rg.search `path`, folio.read `file_path`, folio.glob `path`: kind `path`;
  `path` carries `resolve: cwd` for rg.search and folio.glob, so a CWD default
  is presented resolved. Matches.
- folio.glob `pattern`: stays input, but the enforcer refuses an absolute
  pattern or one with a `..` segment (RFC 0018 section 7). Gap 3 is settled:
  the `Call` builder must apply that refusal, not type the pattern as a path.
- smith.issue-get / pr-get: `repo` and `host` are kind `input` with
  `resolve: cwd-remote`. Settles the smith rows and the "present resolved or
  refuse" question; `Call` needs the cwd's git remote.
- async / batch: every field is kind `route` and `presents: wrapped-call`, as
  expected.
- moxy's own `grit` server IS listed (`status`, `diff`, `log`, `branch`,
  `stage`, ...), with `paths` relative to repo root or `repo_path`. Gap 1 is
  half settled: the D2 samples' `grit_stage`/`grit_branch` are the spinclass
  plugin's (also listed, `resolve: session-worktree`); moxy's are separate
  rows. How `subcommand` maps to `tool-arg-v1` is still not in the file.
- man.list/toc/section: not listed, so all fields are `tool-input-v1`, as drafted.
- Not in the file and still open: `man://` resources (gap 4), and the 9.4
  manifest-key idea (the file's `resolve`, `relative_to` and `split` keys are
  more than `[resources.<field>]` carries today, which strengthens the case
  that a manifest key would need to grow to replace the copy).

## Not done

- No code, no manifest changes, no `catalog.json` copy (to be done when #489
  is dispatched and `Evaluate` exists).
