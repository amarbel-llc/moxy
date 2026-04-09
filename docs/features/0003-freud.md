---
date: 2026-04-08
promotion-criteria: freud serves `freud://sessions` and `freud://sessions/{project}`
  from a live `~/.claude/projects/` tree, installed as a moxy child server, with
  project paths correctly resolved from JSONL `cwd` fields in a hand-verified
  session
status: accepted
---

# Freud: MCP server for past Claude Code sessions

## Problem Statement

Agents working in a Claude Code session have no way to look back at prior
sessions — what was discussed, what was decided, what was tried. The transcripts
exist on disk at `~/.claude/projects/<project-dir>/<session-id>.jsonl`, but
there is no structured access path for an agent to query them.

Freud is now a native config-as-server (`.moxy/servers/freud.toml` +
`.moxy/bin/freud-*` Python scripts). The original Go binary (`cmd/freud`) has
been removed. The native implementation provides structured transcript access
tools that embed the Claude Code JSONL schema, so agents get flattened,
filterable output without needing jq.

## Scope

This doc has landed in two phases.

**Phase 1a** — listing only:

- `freud://sessions` — list all sessions across all projects, most-recent first.
- `freud://sessions/{project}` — list sessions for one project.

**Phase 1b** — transcript content (see the "Phase 1b: Transcript Read"
section below):

- `freud://transcript/{session_id}` — return the raw JSONL transcript for a
  single session, looked up by session id alone.

Grep-style search, tools for summarization/diff, and semantic search are
still **deferred** to later phases tracked in #33. Individual future-work
items (filtered rendering, transcript pagination/progressive disclosure,
role/since filters, a session-id index) are captured in the "Future work"
subsection at the end of Phase 1b.

## Data Source

```
~/.claude/projects/
  <project-dir>/
    <session-id>.jsonl          # transcript, one JSON object per line
    <session-id>/
      tool-results/             # cached large tool outputs, not read in phase 1a
```

`<project-dir>` is the session's `cwd` with every `/` and `.` replaced by `-`,
giving names like `-home-sasha-eng-repos-moxy--worktrees-smart-fir`. **This
encoding is lossy**: a real hyphen and a dot both become `-`, so reversing the
encoding from the dir name alone is ambiguous.

**Resolution:** each `user`-type line in a session JSONL carries a `cwd` field
holding the real absolute path. Freud reads this as ground truth. The dir-name
heuristic is only a fallback for sessions where no line has a usable `cwd`
(empty file, corrupt JSON, purely system messages).

## Resource Shape

### `freud://sessions`

Returns a **fixed-width columnar text** listing, sorted by most-recent activity
first. Columns:

| SESSION | LAST ACTIVITY | MSGS | SIZE | PROJECT |
|---------|---------------|------|------|---------|

- **SESSION** — session id (filename stem), truncated with `…` if the column
  overflows
- **LAST ACTIVITY** — mtime of the JSONL file, formatted `YYYY-MM-DD HH:MM`
- **MSGS** — JSONL line count (cheap, approximate — counts snapshots and
  system messages; noted in a header legend)
- **SIZE** — human-readable bytes (`18K`, `412K`, …)
- **PROJECT** — resolved absolute path (from JSONL `cwd`), or raw dir name
  suffixed with ` (heuristic)` when fallback was used

Example:

```
SESSION                               LAST ACTIVITY     MSGS   SIZE   PROJECT
5441e35e-7fe7-495b-a337-4e406…       2026-04-08 10:57   42     18K    /home/sasha/eng/repos/moxy/.worktrees/smart-fir
93a4213b-73c1-4b99-9970-d4cfc…       2026-04-07 22:14  318    412K   /home/sasha/eng/repos/bob
```

**Future `?format` query param** — reserved for later phases. Anticipated
values: `columnar` (default, current), `tsv` (machine-parseable),
`yaml` (self-describing). Not implemented in Phase 1a, but the parser should
reject unknown `?format=` values with a clear error so the addition is
forward-compatible.

**Progressive disclosure:** if the total list exceeds the configured row
threshold, return a head+tail summary with the full list available via a
`?offset=N&limit=M` query on the same URI — same pattern folio uses for
`folio://read`.

### `freud://sessions/{project}`

Same shape, but filtered to one project. The `{project}` segment accepts
either:

- a raw project-dir name (e.g. `-home-sasha-eng-repos-moxy`), matched
  exactly against `~/.claude/projects/*`, or
- a URL-encoded absolute path (e.g. `%2Fhome%2Fsasha%2Feng%2Frepos%2Fmoxy`),
  resolved by scanning project dirs whose JSONL `cwd` matches.

If neither matches, return a structured error listing known projects (agent
discovery aid, same pattern moxy uses for unknown resource reads).

## Project Path Resolution

Implemented as a two-step process, cached in-memory for the life of the
server process:

1. **Scan step** — walk `~/.claude/projects/*`; for each dir, attempt to open
   the most-recent session JSONL and pull the first line with a non-empty
   `cwd`. That becomes the canonical project path for the dir.
2. **Fallback step** — if no JSONL in the dir yields a `cwd`, apply the
   heuristic decode (`leading -` → `/`, each `-` → `/`, `--` → ambiguous →
   pick `/`). Flag the row as `path: <name> (heuristic)` so the agent knows
   it's approximate.

Cache is invalidated when a project dir's mtime changes. Cheap enough to
re-scan on demand for phase 1a — no index file, no background watcher.

**Known cache-staleness edge case:** keying on directory mtime assumes any
relevant change inside the dir bumps the dir mtime. This holds on standard
Linux filesystems (ext4, btrfs, xfs, tmpfs) for file create/delete/rename,
which covers everything Claude Code does. Failure modes:

- Some network and FUSE filesystems coalesce or skip directory mtime updates;
  on those, a newly added JSONL might not trigger re-resolution.
- The `cwd` for a project dir is invariant in practice (every session in
  `~/.claude/projects/-home-sasha-eng-repos-moxy` has `cwd:
  /home/sasha/eng/repos/moxy`), so a stale cache returns the *correct*
  answer in the realistic case.
- The one scenario where staleness becomes visible: the first session in a
  dir has no usable `cwd` (system-only messages), the cache stores a
  `(heuristic)` answer, and a later session in the same dir contains a real
  `cwd`. The cache never upgrades from heuristic → resolved until something
  else touches the dir mtime.

Phase 1a accepts this — degraded resolution on a rare edge case, never
missing data. If it bites, fixes range from per-file mtime caching (more
stat syscalls) to periodic full invalidation to dropping the cache entirely.

## Non-Goals (Phase 1a)

- Reading session content (`freud://session/{id}`) — landed in Phase 1b
- Search, grep, or semantic retrieval
- Write access or mutation of session files
- Watching for live session updates (each resource read re-scans)
- Decoding `tool-results/` subdirectories
- Any tools — resources only

## Phase 1b: Transcript Read

Tracked as amarbel-llc/moxy#35. Adds a single resource for reading the
content of one past session, completing the minimum viable workflow that
freud's problem statement promises ("look back at prior sessions").

### Resource

- `freud://transcript/{session_id}` — return the raw JSONL transcript for a
  single session, looked up by session id alone.

The `transcript/` root is intentionally separate from `sessions/`. Reusing
`freud://sessions/{thing}` for both project filters and session reads would
require disambiguating the segment as "looks like a UUID vs looks like a
project name," which is fragile.

### Lookup

Session ids are UUIDs and (empirically) globally unique across
`~/.claude/projects/*`. Phase 1b walks the project cache on each read,
opening directories until it finds one containing `<id>.jsonl`. This is
O(N projects) per read but cheap enough for the realistic case (a few
hundred dirs, single stat per dir). A session-id index can be added later
if reads become hot — premature now.

### Rendering

Phase 1b returns the **raw, untransformed JSONL** as a single text content
block. No filtering, no rendering, no markdown, no message-type triage.
The agent gets the file as it exists on disk.

This deliberately punts on the rendering question. From inspecting a real
session JSONL, transcripts contain seven distinct message types:
`user`, `assistant`, `system`, `permission-mode`, `file-history-snapshot`,
`attachment`, `queue-operation`. Of these, the metadata types
(`file-history-snapshot`, `system`, `permission-mode`, etc.) are pure
noise for the "review past session" use case and account for ~15% of
lines in a typical transcript. `user` messages themselves are bimodal:
real human input as plain strings, vs tool results as embedded JSON
blobs. `assistant` content blocks split into `text`, `thinking`, and
`tool_use`.

A future revision should ship a filtered or rendered view as the default,
keeping `?format=raw` as the escape hatch. The rendering choices need
real-world feedback to get right, so deferring is correct.

### Future work

- **Filtered or rendered default view.** Markdown with role headers,
  tool_use as one-line summaries, tool_result elided. Add `?format=`
  values: `raw` (current), `filtered` (drop metadata types), `markdown`
  (rendered).
- **Pagination and progressive disclosure.** Phase 1b reads the entire
  file. Large transcripts (multi-MB JSONL) will blow context. Examine
  the patterns already used elsewhere in the moxy ecosystem before
  picking one:
  - Folio's `folio://read`: head N lines + tail M lines + a continuation
    URI with `?offset/&limit` for paged access.
  - Maneater's `exec`: stash full output to a session-scoped on-disk
    cache, return a summary plus a `maneater.exec://results/{session}/{id}`
    URI the agent can fetch on demand.
  - Built-in cursor pagination via `?offset=N&limit=M` (what
    `freud://sessions` already uses for listings).
  Each has different tradeoffs (latency, hit rate, namespace pollution,
  cache GC). Don't pick one in advance — let real usage tell us which
  matches the read pattern.
- **Filters.** `?role=user|assistant`, `?include=text,tool_use,thinking`,
  `?since=<timestamp>`. Cheap to add once rendering is settled.
- **Session-id index.** Built lazily during `scanProjects` for O(1)
  lookups. Only worth it if transcript reads become hot.

## Architecture

Freud is a native config-as-server: a TOML config at `.moxy/servers/freud.toml`
declares tools backed by Python scripts in `.moxy/bin/`:

```
.moxy/servers/freud.toml    # tool declarations and input schemas
.moxy/bin/freud-sessions    # list sessions (columnar text)
.moxy/bin/freud-transcript  # raw JSONL dump
.moxy/bin/freud-messages    # flattened messages with filters
.moxy/bin/freud-tool-usage  # tool call extraction
.moxy/bin/freud-summary     # session overview
```

Scripts use `#!/usr/bin/env nix` with `nix shell nixpkgs#python3` for
zero-dependency execution. Moxy's native server machinery handles MCP
protocol, namespacing, result caching, and resource-as-fd composition.

## Resolved Decisions

These were worked through with the user on 2026-04-08:

1. **Listing format: fixed-width columnar text.** Most scannable in a Claude
   context window; matches the existing folio/maneater output feel. A
   `?format=` query param is reserved for a later phase (`tsv`, `yaml`) and
   Phase 1a must reject unknown values with a clear error so the addition is
   non-breaking.
2. **Sort key: JSONL file mtime.** Cheap (single stat per file), accurate
   enough for normal use. Revisit if copy/touch drift proves misleading.
3. **Cross-worktree sessions: keep split per project dir.** Matches on-disk
   reality, no git dependency, no shell-out. A worktree is its own project.
4. **`tool-results/` sidecar: omit from row metadata in Phase 1a.** Surfacing
   it invites questions the phase isn't ready to answer (how to read it,
   how to relate it to messages). Add later only if its absence proves
   confusing in real use.

## Dev Testing

The native freud tools can be tested directly:

```sh
.moxy/bin/freud-sessions                           # list all sessions
.moxy/bin/freud-sessions moxy                      # filter by project
.moxy/bin/freud-summary <session-id>               # session overview
.moxy/bin/freud-messages <session-id> user man      # user messages matching "man"
.moxy/bin/freud-tool-usage <session-id> jq          # jq tool calls
```

When running through moxy, tools appear as `freud_sessions`,
`freud_messages`, etc.

## More Information

- Issue: amarbel-llc/moxy#33
- Reference implementations in this repo: `cmd/maneater/` (resource
  template registration, dynamic Instructions field), `.moxy/servers/folio.toml`
  (native server tool declarations).
