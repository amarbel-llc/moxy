---
status: experimental
date: 2026-06-20
promotion-criteria: a clown plugin-host launch over moxy's HTTP transport logs
  `appended dynamic prompt fragments count=1`, and an agent session started
  with a failed child server (e.g. nebulous) sees that failure in its system
  prompt rather than discovering it only when a tool call fails
---

# Dynamic system-prompt fragment (`/clown/system-prompt`)

## Problem Statement

moxy's child-server set varies per launch: which servers connect, their tool
counts, and — most importantly — which ones *failed to start*. A failed child
(e.g. `nebulous` exiting on init) is visible in moxy's own startup logs but
invisible to the agent until it tries a tool and gets an error. The static
build-time prompt fragments (FDR-0003, `.clown-plugin/system-prompt-append.d/`)
structurally cannot express this live state.

clown's plugin protocol (RFC-0002 §5) adds dynamic, plugin-contributed
system-prompt fragments: after a plugin server is healthy and before claude is
exec'd, clown `GET`s the server's declared `systemPromptPath` and appends a
`200` body last to claude's system prompt. moxy serves native HTTP, so it can
answer this itself with no stdio-bridge `prompts/get` plumbing.

## Interface

- **clown.json**: `httpServers.moxy.systemPromptPath = "/clown/system-prompt"`.
- **Endpoint**: `GET /clown/system-prompt` on moxy's streamable-HTTP server.
  - `200` + `text/markdown` body: the fragment, when there is at least one
    child server (connected or failed).
  - `204`: nothing to add (no child servers at all). clown degrades to the
    static prompt.
  - non-`GET`: `405`.
- **Fragment content** (`proxy.FormatSystemPromptFragment`): a "Failed to start
  this session" list (name + error) when any child failed, a one-line
  "Connected: name (N tools), …" roster, and a closing hint pointing at the
  `moxy://tools/{server}` discovery resource and the `madder://blobs/<digest>`
  output convention.

The fragment is computed once at startup from the same `CollectServerSummaries`
result that builds the MCP `Instructions` field, and threaded into the HTTP
server (`streamhttp.Options.SystemPromptFragment`). clown fetches it exactly
once per session, so a startup-time snapshot is the live state at the moment
that matters.

## Limitations

- Snapshot at startup, not per-request. A child that dies *after* startup but
  before clown's fetch is not reflected (the window is sub-second and clown
  fetches immediately after health).
- HTTP transport only — the stdio (`serve-mcp`) path has no clown HTTP fetch and
  carries the same state in the MCP `Instructions` field instead.
- Best-effort on clown's side: a non-`200`/`204` or timeout never blocks the
  launch (RFC-0002 §5).
