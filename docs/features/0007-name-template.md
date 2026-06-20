---
status: experimental
date: 2026-06-20
promotion-criteria: a moxy origin fronting a resource-bearing child for the
  claude.ai web UI runs `serve-http --listen … --name-template '{server}_{tool}'`,
  its tools/list advertises dot-free child tool names accepted by claude.ai's
  `^[a-zA-Z0-9_-]{1,64}$` validator, a tools/call to a rendered name round-trips
  to the child's original tool, and a colliding template
  (`--name-template '{tool}'` with two servers sharing a tool name) is rejected
  at startup — letting an origin advertise callable tools instead of falling
  back to `--expose resources-only` (moxy#375)
---

# Configurable tool/prompt name template (`serve-http --name-template`)

## Problem Statement

moxy namespaces every child tool and prompt name as `<server>.<tool>` (the dot
join) and recovers `(server, tool)` on dispatch by splitting on the first dot
(`splitPrefix`). claude.ai's web frontend validates every advertised **tool**
name against `^[a-zA-Z0-9_-]{1,64}$` and rejects the *whole* connection if any
name contains a dot. So a moxy origin fronting a resource-bearing child (e.g.
caldav CRUD) for the claude.ai web UI cannot advertise its tools at all.

The existing escape, `--expose resources-only` (FDR 0006), hides every tool so
the connection is accepted — but the claude.ai web UI renders only tools, not
native MCP resources, so the UI is left empty. The missing capability is the
ability to advertise **callable tools with claude.ai-safe (dot-free) names**.
This is the non-dotted-tool-names half of moxy#375.

## Interface

### The flag

```
moxy serve-http --listen 127.0.0.1:8731 --name-template '{server}_{tool}'
```

`--name-template` is a string with the placeholders `{server}` and `{tool}` and
arbitrary surrounding literals. Omitting it (or `--name-template '{server}.{tool}'`)
is the current behaviour: the dot join, byte-identical to before. The flag
exists only on `serve-http`; `serve-mcp` (stdio, local) is always the default
template, so the local Claude Code path and its pre-tool-use hook are
unaffected.

Examples:

- `--name-template '{server}_{tool}'` — claude.ai-safe dot-free names
  (`grit_commit`)
- `--name-template '{tool}'` — bare tool names (only valid when no two servers
  share a tool name; collisions are rejected at startup)
- `--name-template '{server}.{tool}'` / no flag — current behaviour

### Orthogonal to `--expose` (do not drop `--expose` on a public origin)

`--name-template` only changes *how names are rendered*; it does **not** change
*which categories of tools are exposed*. The two flags are independent and a
public claude.ai origin needs both. In particular, an **empty `--expose`
resolves to `full`** (child + resource-bridge + meta), so "just drop
`--expose resources-only`" would re-expose moxy's control surface
(`restart`/`async`/`batch`/`status`) on the public origin — the exact boundary
`resources-only` was protecting. The correct migration for an origin moving from
hidden-tools to visible-tools is to **swap the profile, not drop the flag**:

```
# before (FDR 0006): connection accepted, but tools-only UI is empty
moxy serve-http --listen 127.0.0.1:8731 --expose resources-only

# after: dot-free child tools visible AND callable, control surface still hidden
moxy serve-http --listen 127.0.0.1:8731 \
  --name-template '{server}_{tool}' \
  --expose no-meta,-resource-bridge
```

`no-meta` keeps the control plane hidden; `-resource-bridge` drops the generic
`<child>_resource-read`/`-templates` tools (a child's purpose-built read tool, or
native resources, cover reads instead). The `--expose` filter still gates
correctly under a custom template because categories are carried on the registry
entry, not parsed from the rendered name.

### Parse rules (an intentional asymmetry)

`{tool}` is **required**; `{server}` is **optional**. Dropping `{tool}` would
collapse every tool of a server to one name — an intra-server collision that is
never wanted and is cheap to reject statically (at flag-parse time, before any
child is spawned). Dropping `{server}` is a legitimate "bare names" request
whose collisions are data-dependent (two servers sharing a tool name) and are
caught at build time, not parse time. Unknown placeholders and unterminated
braces are also parse-time errors.

### Reverse dispatch: a registry, not a parse

An arbitrary template is not parseable back to its inputs (consider `{tool}`
alone, or `{server}_{tool}` where server and tool both contain `_`). So dispatch
does **not** re-parse the rendered string. At list time moxy builds a
**registry** mapping each rendered name to its canonical `Entry`
(`server`, the child's own tool/prompt name, kind, and exposure category). The
proxy keeps the historical `splitPrefix` fast path under the default template
(zero behaviour change for every existing deployment) and consults the registry
only under a custom template — for tool dispatch, prompt dispatch, the `--expose`
category gate, and statsd segments alike.

The exposure category is **carried on the registry entry**, computed where the
name is built (child / resource-bridge / failed-status), never re-derived from
the rendered string — which is what keeps `--expose` correct under a custom
template (a dot-free `madder-mcp_resource-read` would otherwise be misclassified
Meta by name parsing).

### Collision handling

Collisions depend on the live `(server, tool)` set, known only after children
are spawned and ephemerals probed. So:

- **Malformed template** (bad placeholder / missing `{tool}` / unterminated
  brace) → fails fast at flag parse, before any bootstrap.
- **Valid but colliding template** → detected by a one-shot registry build after
  `ProbeEphemeral`; `serve-http` refuses to serve rather than silently shadow a
  tool on what is often a public origin.

### `moxy render`

`moxy render --name-template T --server S --tool N` prints the advertised name
(forward); `moxy render --name-template T --resolve <rendered>` prints the
canonical `server<TAB>tool` via a structural inverse. `render` does **not**
introspect a running server — it takes its own `--name-template` and the caller
must pass the same template `serve-http` runs. It is the renderer's
out-of-process surface, intended for the pre-tool-use hook to consume in the
later moxyfile-key phases (see Limitations).

## Rationale

- **One template instead of a separator flag.** An arbitrary `{server}`/`{tool}`
  template (with a build-time collision check) is strictly more expressive than
  "pick the separator", and the registry-based reverse falls out of it cleanly.
- **Default keeps the fast path.** Forward rendering of the default template is
  byte-identical to the dot join, and the proxy keeps `splitPrefix` for
  dispatch/category/statsd under the default — so serve-mcp and every existing
  serve-http deployment behave exactly as before; the registry path only
  activates under `--name-template`.
- **Decision at the launch site**, co-located with the public-exposure decision
  (`serve-http --listen … --expose … --name-template …`), like `--expose`.

## Limitations

- **serve-http only, no moxyfile key in this phase.** Per-server / per-moxin /
  global moxyfile keys are deliberately deferred to later phases; the renderer is
  built reusable so they slot in. The pre-tool-use hook is **not** rewired here
  (serve-http does not trigger the local Claude Code hook); when the moxyfile-key
  phases let the local serve-mcp path carry a custom template, the hook will
  resolve names via `moxy render`.
- **Startup collisions fail fast; reload-introduced collisions degrade.** A
  collision present at startup refuses to serve. A collision introduced *after*
  startup by a `restart`/reload cannot fail-fast (the server is already serving);
  it degrades by dropping the later entry (first-wins) and logging a loud stderr
  warning. The "build-time check" guarantee is therefore softer across a live
  reload than at startup.
- **`moxy render --resolve` is structural-only.** A standalone `render` process
  lacks the live `(server, tool)` set, so reverse resolution uses a structural
  inverse that works only for invertible templates (exactly one `{server}` and
  one `{tool}` separated by a literal). The exact registry-backed reverse lives
  only in the proxy, where the pair set exists.
- **Resource URIs are out of scope.** They keep their `<server>/<uri>` form and
  are served natively; only tool and prompt names are templated.
