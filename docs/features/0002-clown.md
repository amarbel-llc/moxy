---
date: 2026-04-05
promotion-criteria: folio server replaces Read/Write/Edit/Grep/Glob for at least
  one user's full Claude Code session without fallback to built-ins
status: exploring
---

# Clown: MCP replacements for Claude Code built-in tools

## Problem Statement

Claude Code ships with hardcoded built-in tools (Read, Write, Edit, Grep, Glob,
Bash) that cannot be disabled or replaced. These tools lack progressive
disclosure, have no permission model beyond the harness's allow/deny, and can't
be extended or composed with other MCP servers.

The moxy ecosystem already replaces two of these:

- **Bash** → maneater (man-page-aware exec with permission rules and progressive
  disclosure)
- **Git subset of Bash** → grit (structured git operations as resources + tools)

Replacing the rest would give users:

1.  **Progressive disclosure** --- large file reads and search results
    summarized first, full content available via resource URIs (same pattern as
    maneater)
2.  **Composability** --- file operations visible to other MCP servers and
    intermediaries like moxy (annotation filtering, pagination, logging)
3.  **Permission rules** --- fine-grained allow/deny on file paths, not just
    tool-level approval
4.  **Consistency** --- all tools go through the same MCP protocol, same moxy
    proxy, same annotation system

## Replacement Plan

  Built-in            MCP replacement   Status
  ------------------- ----------------- ---------
  Bash                maneater          shipped
  git (via Bash)      grit              shipped
  GitHub (via Bash)   get-hubbed        shipped
  Read                folio             planned
  Write               folio             planned
  Edit                folio             planned
  Grep                folio             planned
  Glob                folio             planned

### Folio

File I/O and search MCP server. Read operations as resources (progressive
disclosure for large files), mutations as tools.

**Resources:**

- `folio://read?path={path}&offset={offset}&limit={limit}` --- read file with
  line numbers, offset/limit pagination
- `folio://glob?pattern={pattern}&path={path}` --- file pattern matching
- `folio://grep?pattern={pattern}&path={path}&type={type}&glob={glob}` ---
  content search (ripgrep-backed)

**Tools:**

- `write` --- create or overwrite a file
- `edit` --- exact string replacement in a file (same semantics as Claude Code's
  Edit: unique match required, `replace_all` flag for global replace)

**Progressive disclosure:** file reads and search results that exceed a token
threshold return a summary + resource URI for the full content, same pattern as
maneater exec.

**Permission rules:** path-based allow/deny in `folio.toml`, same hierarchy
pattern as maneater.toml.

## Blockers

### Claude Code cannot disable built-in tools

Even with perfect MCP replacements, Claude Code's system prompt instructs it to
prefer built-in tools. Without a mechanism to disable them (e.g.,
`disable_tools` in settings.json), the MCP versions will always compete with
built-ins for attention. This is the primary external blocker.

**Workaround:** CLAUDE.md instructions to prefer folio over built-ins. Fragile
but functional for testing.

### Harness-level guardrails

Claude Code enforces "must Read before Edit" and "must Read before Write
(existing files)" at the harness level. Options:

- Folio server tracks read state per-path (stateful, but contained)
- Accept that the guardrail lives in CLAUDE.md instructions instead
- Propose MCP annotation extension for tool preconditions

### Binary and special file content

Read handles images (returns visual content), PDFs, and Jupyter notebooks. MCP
supports base64 content in resource responses but this hasn't been tested
end-to-end through moxy. Folio would need to:

- Detect binary files and return base64-encoded content with appropriate MIME
  type
- Handle PDF page extraction
- Handle notebook cell rendering

This is lower priority --- start with text files and iterate.

## Limitations

- Performance: MCP stdio round-trip adds latency vs in-process built-ins.
  Mitigated by keeping persistent server running (not ephemeral).
- No way to enforce read-before-edit at the protocol level without server-side
  state or an MCP extension.
- Image/PDF/notebook support deferred to later iteration.

## More Information

- Maneater's progressive disclosure: `cmd/maneater/` in this repo
- MCP resource protocol: resources are read-only by spec, mutations use tools
- Claude Code built-in tool source: not public, behavior documented in system
  prompts
