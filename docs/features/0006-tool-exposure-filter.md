---
status: experimental
date: 2026-06-18
promotion-criteria: a public authed moxy origin (claude.ai connector behind a
  Cloudflare Access tunnel) runs `serve-http --listen … --expose resources-only`,
  its tools/list advertises zero tools (children's resources flow natively),
  the connection is accepted by claude.ai's name-pattern validator, and a
  client attempt to call a meta tool (restart/async/batch) over that origin is
  rejected — letting the origin drop the per-child `generate-resource-tools =
  false` workaround (#374)
---

# Tool-exposure filter (`serve-http --expose`)

## Problem Statement

When moxy fronts resource-bearing children over a publicly-reachable, authed
HTTP origin (e.g. a Cloudflare Access tunnel exposing moxy to claude.ai web),
moxy's full tool surface is wrong for the deployment in two ways:

1. **Strict frontends hard-reject the connection on dotted tool names.**
   claude.ai's frontend validates every advertised tool name against
   `^[a-zA-Z0-9_-]{1,64}$` and rejects the *whole* connection if any name
   fails. moxy's auto-generated per-child resource-bridge tools
   (`<child>.resource-read` / `<child>.resource-templates`) fail because of the
   `.` namespace separator (moxy(7) NAMESPACING). Claude Code tolerates this (it
   sanitizes `.`→`_`); claude.ai does not. These bridge tools are also redundant
   for a frontend that reads MCP resources natively (`resources/list` +
   `resources/read`).

2. **moxy's control surface should not be reachable from a public client.**
   The meta tools — `restart`, `batch`, `async`, `async-result`, `async-cancel`,
   plus the framework `status` tool from `App.RegisterMCPToolsV1` — let a
   connected client restart the origin's children, dispatch async/batch jobs,
   etc. An origin should expose its children's resources, not moxy's control
   surface.

`generate-resource-tools = false` (per-server) already suppresses (1) but is
clumsy to repeat per child, and nothing addresses (2). Rather than accrete a
pile of one-off `--no-X` booleans, moxy gains one composable filter.

## Interface

### The flag

```
moxy serve-http --listen 127.0.0.1:8731 --expose <selector>[,<selector>...]
```

`--expose` is a comma-separated list of selectors applied left-to-right to a
base set of **tool categories**. Omitting the flag (or `--expose full`) is the
current behaviour: everything is advertised. The flag exists only on
`serve-http`; `serve-mcp` (stdio, local) is always `full`.

### Tool categories

Every tool moxy advertises is classified — by name, deterministically — into
exactly one category:

| category          | matches                                              | examples |
|-------------------|------------------------------------------------------|----------|
| `meta`            | no `.` in the name (moxy/framework builtins)         | `status`, `restart`, `batch`, `async`, `async-result`, `async-cancel` |
| `resource-bridge` | `<server>.resource-read` / `<server>.resource-templates` | `madder-mcp.resource-read` |
| `child`           | any other `<server>.<tool>`                          | `grit.status`, `cutting-garden.foo` |

Resources themselves are served natively via `ResourceProviderV1` and are
**never** gated by this filter — it governs only the `tools/list` surface and
tool-call dispatch.

### Selectors: profiles and tag toggles

A selector is either a **profile** name or a **tag toggle** (`+<cat>` /
`-<cat>`). Resolution starts from `full` and applies each selector in order; a
profile resets the working set, a toggle adjusts it.

Profiles:

| profile          | resolved categories            | equivalent toggles            |
|------------------|--------------------------------|-------------------------------|
| `full` (default) | child, resource-bridge, meta   | —                             |
| `no-meta`        | child, resource-bridge         | `-meta`                       |
| `resources-only` | *(none)*                       | `-child,-resource-bridge,-meta` |

`resources-only` advertises **no tools at all** — the literal reading of the
name and the safest surface for a public origin; the children's resources still
flow natively. Examples:

- `--expose resources-only` — claude.ai connector origin (resources, zero tools)
- `--expose -meta` — keep child + bridge tools, drop the control surface
- `--expose no-meta,+meta` — degenerate but legal (resets to no-meta, re-adds meta = full)
- `--expose full` / no flag — current behaviour

Unknown profile names and unknown category names are hard errors at startup
(fail fast rather than silently advertising too much).

### Enforcement: list and call

The resolved filter lives on the `Proxy`. It is enforced at two points:

- **`ListToolsV1`** — a category whose toggle is off contributes no tools. The
  child-tool loop, the synthetic resource-bridge injection, and the builtin
  (meta) append each consult the filter.
- **`CallToolV1`** — every dispatch is categorized by name and rejected with an
  MCP error result if its category is filtered out. This is what makes `-meta`
  a real boundary: a client that already knows `restart` exists cannot call it
  on a `--expose no-meta` / `resources-only` origin. The single dispatch funnel
  (builtins, batch sub-calls, native moxins, child + ephemeral servers all pass
  through `CallToolV1`) means one gate covers every path.

Because enforcement is uniform across both list and call for all three
categories, there is no "listed-but-uncallable" or "hidden-but-callable"
asymmetry to reason about.

## Rationale

- **One composable mechanism** instead of an accreting pile of `--no-X`
  booleans. Profiles give ergonomic presets for common deployments; tag toggles
  give precise control for the long tail.
- **Decision at the launch site.** The "don't expose my control surface on a
  public origin" choice is explicit and co-located with the public-exposure
  decision (`serve-http --listen … --expose …`), not buried in an inheritable
  moxyfile that is easy to forget. A moxyfile mirror can be added later if a
  need appears; the flag stays authoritative.
- **Classify by name, not by registration site.** The categorizer is a pure
  function of the tool name, so the same logic drives both list filtering and
  call gating with no bookkeeping.

## Limitations

- A child server that itself exposes a tool literally named `resource-read` or
  `resource-templates` would be classified `resource-bridge`. This collides
  with the synthetic-tool naming moxy already special-cases; it is an accepted
  edge.
- The flag is `serve-http`-only by deliberate scope. `serve-mcp` over stdio is
  a local, trusted transport; if a use case for filtering it appears, the
  `Proxy` filter is transport-agnostic and the flag can be lifted to
  `serve-mcp` trivially.
- No moxyfile key in v1 (see Rationale).
