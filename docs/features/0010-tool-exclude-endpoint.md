---
status: experimental
date: 2026-07-07
promotion-criteria: clown's `--cheap-context` picker (amarbel-llc/clown#175)
  drives moxy's tool surface the same way it already drives
  `clown-stdio-bridge`-fronted plugins — POST a name list to
  `/clown/exclude-tools`, confirm the excluded moxins/tools vanish from a live
  Claude Code session's tool list after `/mcp` → Reconnect, and confirm a
  direct `tools/call` for an excluded name is rejected rather than merely
  hidden
---

# Dynamic per-session tool/moxin exclusion (`/clown/exclude-tools`)

## Problem Statement

`amarbel-llc/clown#175`'s `--cheap-context` picker lets a user interactively
choose which MCP tools load for a session, cutting context spent on tool
schemas the session doesn't need. For stdio servers, clown implements this by
wrapping them in `clown-stdio-bridge`, which exposes `POST
/clown/exclude-tools`: a JSON list of excluded tool/plugin names that the
bridge strips out of `tools/list` before Claude Code ever sees them.

moxy can't be wrapped this way. It is declared as a native `httpServers` entry
in `clown.json` — moxy speaks the clown-plugin-protocol handshake and
streamable-HTTP transport itself, so there is no clown-owned bridge process in
moxy's request path for `--cheap-context` to hook into. moxy is also the
primary motivating case: it aggregates ~170 tools across ~17 moxin children,
so trimming what a session sees matters more here than almost anywhere else in
the fleet.

moxy already has a *static* answer with the right granularity —
`disable-moxins` (whole-moxin or per-tool, `internal/config/schema`) — but it
is baked into the tool surface at bootstrap/reload time
(`cmd/moxy/bootstrap.go` filters a moxin's `Tools` before ever constructing
the child, or skips connecting to a disabled `[[servers]]` entry outright).
Toggling that live would mean tearing down and respawning children, which is
far more disruptive than what `--cheap-context` needs: it wants to change what
one session's `tools/list` and `tools/call` accept, not what child processes
are running.

## Interface

### The endpoint

```
GET  /clown/exclude-tools
POST /clown/exclude-tools
```

Alongside `/healthz` and `/clown/system-prompt`
(`docs/features/0007-dynamic-system-prompt-fragment.md`), this is a bespoke,
unauthenticated, clown-only HTTP route on the same `streamhttp.Server` —
not part of the MCP protocol surface itself and not gated by
`Mcp-Session-Id`.

Both methods share one JSON body shape:

```json
{"exclude": ["chix", "folio.write"]}
```

Each entry is either a whole moxin/server name (`"chix"`, excluding every tool
that server owns) or a dotted rendered tool name (`"folio.write"`, excluding
just that one) — the same entry syntax as the moxyfile's `disable-moxins` key.

- **`POST`** replaces the excluded set wholesale. There is no incremental
  add/remove API: the picker always knows the full desired set (it renders
  from a checkbox list), so full-replace keeps the client and server in
  agreement without a separate "clear" operation. Responds `200` with the
  resulting set (echoing what `GET` would now return).
- **`GET`** reads back the current excluded set, for the picker to show
  existing state and for tests.
- If the wired `Tools` provider doesn't implement the optional `ToolExcluder`
  capability (e.g. a plain `server.ToolProviderV1` test double), the route
  responds `501 Not Implemented`.

### Enforcement: list and call

Mirrors `internal/toolfilter`'s `--expose` mechanism
(`docs/features/0006-tool-exposure-filter.md`) — a resolved deny-set consulted
at the same two funnels:

- **`ListToolsV1`** — `Proxy.applyToolExclude` drops any tool whose rendered
  name or owning server is in the exclude set, run immediately after the
  existing `--expose` filter.
- **`CallToolV1`** — the same check gates dispatch, so an excluded tool is
  uncallable even by a client that already knows its name from before the
  exclusion took effect. This is the same "real boundary, not just a list
  filter" property `--expose` already has for categories (see 0006's
  Enforcement section) — a session that hides `chix.*` should not still be
  able to invoke it by name.

`internal/toolexclude.Set` is the deny-set type: two maps (whole-server names,
dotted tool names) built fresh from each POST body by `toolexclude.Parse`.
Owning-server resolution reuses the same source `--expose` already built for
its category resolution — `naming.Registry` under a custom `--name-template`,
or a `.`-split of the rendered name under the default template — so the two
filters never disagree about which server a tool belongs to.

### Change notification

A `POST` that actually changes the resulting set emits
`notifications/tools/list_changed` over any open SSE stream (the same
notification `restart` already emits after a moxin reload). As documented
against `restart`, **Claude Code does not currently act on this notification
client-side** (`anthropics/claude-code#4118`, `#50515`, `#50339`, `#51507`) —
clown's picker still needs to trigger `/mcp` → Reconnect itself after POSTing;
the notification exists for MCP clients that do honor it (and for the bats
test suite, which asserts on it the same way the `restart` SSE tests do).

## Rationale

- **Reuse the `--expose` shape, not the `disable-moxins` mechanism.** Both are
  deny-sets enforced at the same two funnels; the only real difference is
  name-based vs. category-based matching and mutable-at-runtime vs.
  resolved-once-at-startup. Building a second bootstrap-time filtering path
  would have meant either restarting children to change what's excluded (too
  disruptive for an interactive picker) or duplicating `applyToolFilter`'s
  list/call symmetry from scratch.
- **Full-replace over incremental add/remove.** The picker's natural state is
  "the current checked set," not a diff — full-replace matches that directly
  and avoids a separate clear operation or drift between client and server
  state.
- **Unauthenticated, alongside `/clown/system-prompt`.** This is a
  local-network, clown-to-moxy control channel serving the same trust
  boundary as the existing system-prompt route, not a new one.

## Limitations

- No moxyfile-level static equivalent for this specific mutable form — the
  moxyfile already has `disable-moxins`/`disable-servers` for the startup
  case; this endpoint is deliberately runtime-only.
- Exclusion is scoped to the running `streamhttp.Server` process, not
  persisted. A moxy restart clears it; clown is expected to re-POST after a
  session's picker state is known, not rely on moxy remembering across
  process lifetimes.
- Like `--expose`, this governs only the `tools/list`/`tools/call` surface —
  MCP resources are unaffected and continue to flow natively.
