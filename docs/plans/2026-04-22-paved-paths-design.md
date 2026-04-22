# Paved Paths Design

**Date:** 2026-04-22  
**Status:** Approved

## Overview

Paved paths enforce progressive disclosure of tools by requiring agents to complete
ordered stages before unlocking later tools. Moxyfiles define the paths; moxy
enforces them at the proxy layer by filtering `tools/list` responses and tracking
per-session state.

Primary use cases:
- **Repo-specific workflow enforcement** — e.g., require reading docs before editing
- **Agent onboarding** — guide a new agent through orientation before granting full access

## Moxyfile Config Schema

A new top-level `[[paved-paths]]` array. Each path has a name, description, and
ordered list of stages. Each stage has a label and a list of tools that become
visible when that stage is active.

```toml
[[paved-paths]]
name = "onboarding"
description = "Learn the repo before making changes"

  [[paved-paths.stages]]
  label = "orient"
  tools = ["folio.read", "folio.glob", "rg.search"]

  [[paved-paths.stages]]
  label = "understand"
  tools = ["hamster.doc", "man.toc", "man.section"]

  [[paved-paths.stages]]
  label = "edit"
  tools = ["folio.write", "grit.commit", "grit.push"]
```

Multiple paths may be defined. If more than one path exists the agent must select
one; if only one is defined it is implicitly required (hybrid model).

## State Machine

**State shape** (in-memory for prototype; see Persistence section):

```json
{
  "selected_path": "onboarding",
  "current_stage": 0,
  "called_tools": ["folio.read"]
}
```

**Transitions:**

| State | `tools/list` returns | Trigger to advance |
|---|---|---|
| No path selected | `paved-paths` only | Agent calls `paved-paths` with `select` arg |
| Stage N active | tools for `stages[N]` + `paved-paths` | Any tool in stage N is called |
| Path complete | All tools | — |

On each transition moxy emits a `tools/listChanged` notification so the client
refreshes its tool list.

## `tools/listChanged` Notification

Currently moxy declares `Capabilities.Tools.ListChanged: true` but never emits
the notification. This feature adds a `p.notifyToolsChanged()` helper on `Proxy`
that constructs the `tools/listChanged` JSON-RPC notification and calls
`p.notifier`. It is called at each paved-path state transition. Scope is limited
to paved-paths for now; can be generalized later (e.g., on server restart).

## Meta Tools

Two new builtin meta tools registered alongside `restart` and `exec-mcp`:

### `paved-paths`

Always visible (even before path selection — it is the *only* visible tool before
selection, and remains visible throughout).

Behavior by call pattern:

- **No args, no path selected:** Returns list of available paths (name +
  description). If only one path is configured, also returns a prompt to select it.
- **`select: "<name>"` arg:** Selects the named path, transitions state, emits
  `tools/listChanged`, returns confirmation + description of the first stage's tools.
- **No args, path selected:** Returns current path name, current stage label, and
  which tools in the stage have been called vs. still needed.

## Persistence

**Prototype:** In-memory only. State is lost on moxy restart.

**Production target:** Persist to `~/.local/state/moxy/<session-id>/paved-path.json`
using atomic write (tempfile + rename). Loaded at session start using the existing
session ID resolution chain (`CLAUDE_SESSION_ID` → `SPINCLASS_SESSION_ID` →
generated UUID). Written on each state transition.

Human-in-the-loop path selection via elicitation mechanisms is a future
enhancement pending verification that the elicitation flow works end-to-end.

## Implementation Sequence

1. Add `[[paved-paths]]` parsing to `internal/config`
2. Implement `p.notifyToolsChanged()` and wire `tools/listChanged` emission
3. Add paved-path state struct to `Proxy` (in-memory)
4. Filter `ListToolsV1` based on paved-path state
5. Register `paved-paths` meta tool; implement select + status handlers
6. Check for stage advancement in `CallToolV1` after each successful tool call
7. Add bats integration tests
8. (Follow-up) Add disk persistence to `~/.local/state/moxy/`

## Open Questions (for FDR)

- **Stage advancement:** Should calling *any* tool in a stage advance to the next,
  or must *all* tools in the stage be called? Start simple: any tool advances.
- **Path completion:** Should completing a path unlock *all* tools (no more
  filtering) or only the last stage's tools? Start simple: all tools.
- **Multiple paths:** Are all defined paths shown to the agent at once, or only
  the first? Start simple: all shown.
- **Always-available tools:** Should moxyfile support marking certain tools as
  always visible regardless of path state? Flagged as future option (Option B
  from design exploration).
- **Milestone constraints:** Future enhancement — milestones based on tool
  arguments or return values, not just tool name called.
- **Human-in-the-loop selection:** Future enhancement — path selection via
  elicitation mechanism (pending verification).
