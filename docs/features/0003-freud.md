---
date: 2026-04-08
promotion-criteria: freud serves `freud://sessions` and `freud://sessions/{project}`
  from a live `~/.claude/projects/` tree, installed as a moxy child server, with
  project paths correctly resolved from JSONL `cwd` fields in a hand-verified
  session
status: exploring
---

# Freud: MCP server for past Claude Code sessions

## Problem Statement

Agents working in a Claude Code session have no way to look back at prior
sessions — what was discussed, what was decided, what was tried. The transcripts
exist on disk at `~/.claude/projects/<project-dir>/<session-id>.jsonl`, but
there is no structured access path for an agent to query them.

Freud is a new MCP server (`cmd/freud`) that exposes these transcripts as
read-only MCP resources. It follows the maneater/folio pattern: a single
binary, `freud serve mcp` as the entry point, resources for reads, progressive
disclosure for large responses.

## Scope (Phase 1a)

This doc covers the smallest useful slice:

- `freud://sessions` — list all sessions across all projects, most-recent first.
- `freud://sessions/{project}` — list sessions for one project.

Reading a single session's content (`freud://session/{id}`), grep-style search,
tools for summarization/diff, and semantic search are **deferred** to later
phases tracked in #33. This doc will be extended (or a follow-up FDR filed)
when those land.

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

## Non-Goals (Phase 1a)

- Reading session content (`freud://session/{id}`)
- Search, grep, or semantic retrieval
- Write access or mutation of session files
- Watching for live session updates (each resource read re-scans)
- Decoding `tool-results/` subdirectories
- Any tools — resources only

## Architecture Sketch

New binary `cmd/freud/` following `cmd/folio/`'s layout:

```
cmd/freud/
  main.go          # entry point, `freud serve mcp` dispatch
  server.go        # freudServer struct, ResourceProviderV1 impl
  sessions.go      # scan, list, sort, format
  project.go       # project-dir scanning, cwd resolution, heuristic decode
  config.go        # freud.toml hierarchy (same pattern as folio/maneater)
  *_test.go        # go tests with a temp HOME fixture
```

Config hierarchy (`freud.toml`), mirroring folio/maneater:

1. `~/.config/freud/freud.toml`
2. each parent dir between `$HOME` and `$CWD`
3. `./freud.toml`

Initial config surface:

```toml
# Override the default ~/.claude/projects location
projects-dir = "~/.claude/projects"

[list]
max-rows  = 500   # progressive-disclosure threshold for `freud://sessions`
head-rows = 50
tail-rows = 20
```

No permissions section yet — the entire `~/.claude/projects` tree is readable
by default. If/when we add content reads in a later phase, we'll layer on
path-based allow/deny like folio.

## Integration with Moxy

Added to the top-level moxyfile as a persistent child server:

```toml
[servers.freud]
command = "freud"
args = ["serve", "mcp"]
```

Built as a separate Nix package (`packages.freud`) alongside `maneater` and
`folio`, included in the top-level `symlinkJoin` via `repo-packages.nix`.

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

## More Information

- Issue: amarbel-llc/moxy#33
- Reference implementations in this repo: `cmd/folio/` (resource patterns,
  config hierarchy, progressive disclosure), `cmd/maneater/` (resource
  template registration, dynamic Instructions field).
