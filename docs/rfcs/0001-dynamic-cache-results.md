---
status: proposed
date: 2026-07-31
---

# Dynamic cache-results via result `_meta` (`cache-results = "dynamic"`)

## Abstract

This document specifies a per-invocation caching-policy mechanism for moxy
native moxin tools. A tool MAY set `cache-results = "dynamic"` and, in each MCP
result it emits, carry a `_meta` field naming the cache mode (`always`,
`threshold`, or `never`) that moxy MUST apply to that result. The cache intent
travels inline with the data that determines it — the tool decides as it emits —
rather than being computed by a separate predicate before the tool runs. This
lets a single family tool vary its caching per subcommand (e.g. a `diff`
subcommand that always caches while its siblings use the threshold default),
which the static, per-tool `cache-results` field (#319) cannot express.

## Introduction

Native moxin tools declare a `cache-results` policy — `always`, `threshold`
(default), or `never` — governing whether output is written to the madder blob
store and surfaced as a resource URI (#319). The policy is resolved once at
config-load time and is fixed for the life of the tool.

The capability-family design (FDR 0012) collapses several former single-purpose
tools into one family tool discriminated by a `subcommand` argument. When a
family absorbs a tool that needed `always` (e.g. `diff`, whose large outputs are
composable and should always be blob-cached) alongside siblings that want the
`threshold` default (e.g. `status`, `log`), the single per-tool `cache-results`
field cannot serve both.

Two mechanisms were considered. A separate predicate command (mirroring
`perms-request = "dynamic"` / `[dynamic-perms]`) would run *before* the tool and
compute the mode from the arguments — but it spawns a second process per call
and duplicates argument wiring, and it cannot see the actual output. This
specification instead adopts an **inline** mechanism: the tool decorates its own
result envelope with the cache intent, so the decision lives with the data that
informs it and no extra process runs. The trade-off is that inline signalling
requires the tool to own its MCP envelope, i.e. `result-type = "mcp-result"`.

Scope: the native moxin config format, the `mcp-result` result-shaping path, and
the `_meta` field contract between a moxin and moxy. This does not change the
meaning of the three static cache modes, nor the `text`-mode result path.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD",
"SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be
interpreted as described in RFC 2119.

## Specification

### 1. The `cache-results = "dynamic"` value

The `cache-results` field gains a fourth value, `dynamic`, alongside `always`,
`threshold`, `never` (absent meaning `threshold`).

- A tool with `cache-results = "dynamic"` MUST also set
  `result-type = "mcp-result"`. Config loading MUST fail with an error naming
  the tool file if `cache-results = "dynamic"` is set under any other
  `result-type` (including the default `text`), because a text-mode tool's
  stdout *is* the cached payload and has no envelope to carry `_meta`.
- Any `cache-results` value other than `always`, `threshold`, `never`, or
  `dynamic` MUST be rejected at config-load time.
- `dynamic` is purely an opt-in switch: it tells moxy to honor a `_meta` cache
  intent (Section 2) in the tool's results. It requires no additional config
  block.

### 2. The result `_meta` cache-intent field

When a tool's resolved policy is `dynamic`, moxy MUST inspect the `_meta` object
of each MCP result the tool emits for the key `moxy/cache`:

```json
{
  "content": [ { "type": "text", "text": "…" } ],
  "_meta": { "moxy/cache": "always" }
}
```

- The value MUST be one of the strings `always`, `threshold`, or `never`.
- `moxy/cache` is a **result-level** signal in this version: it selects the cache
  mode for the entire result — every cacheable content block is treated as if
  the tool had declared that static `cache-results` value for this call.
- The resolved mode governs caching exactly as the equivalent static
  `cache-results` value would in `mcp-result` mode, which today is **mime-gated**:
  a text block carrying a `mimeType` is elevated to a cached resource block under
  `always`, while `threshold`/`never` leave it inline and drop the mime (#319). A
  text block with no `mimeType` is not cached in `mcp-result` mode regardless of
  mode; extending `always` to cache mime-less blocks is deferred (see
  Compatibility).
- `_meta` follows the MCP reserved-key convention (keys beginning with `_`).
  moxy MUST remove the `moxy/cache` entry (and an emptied `_meta` object) from
  the result before returning it to the client, so the private signal never
  leaks downstream.
- A `moxy/cache` value that is absent, not a string, or not one of the three
  modes MUST cause moxy to fall back per Section 3. moxy MUST NOT reject the
  tool call for a malformed cache intent — caching policy is advisory and never
  fails the call.

### 3. Fallback

moxy MUST resolve to `threshold` (the global default) when a `dynamic`-policy
tool's result carries no valid `moxy/cache` intent (absent, wrong type, or an
unrecognized value). This reproduces the behavior of a tool that declared no
`cache-results` at all.

Rationale (informative): `threshold` is the safe degrade — a missing or
malformed intent neither floods the agent context (as a `never` fallback could
with large output) nor over-writes the blob store (as `always` would).

### 4. Interaction with other fields

- `content-type` remains a pure mime label; the resolved mode governs caching
  exactly as the static modes do (a resolved `never`/`threshold` for a small
  output still drops the mime, per #319).
- `no-truncate` and `substitute-result-uris` act on whatever resource/text
  block the resolved mode produces; they are unaffected by this mechanism.
- The intent applies only to the `mcp-result` shaping path. The `text`-mode
  path (moxy builds the envelope from raw stdout) is out of scope and
  unreachable, since `dynamic` requires `mcp-result` (Section 1).

### 5. Examples

A family tool that caches only its `diff` subcommand always:

```toml
schema = 3
result-type = "mcp-result"
cache-results = "dynamic"
command = "@BIN@/stage"
# ... subcommand enum add|unstage|rm|mv|status|diff ...
```

The `diff` branch of the `stage` script emits (always-cache):

```json
{ "content": [ { "type": "text", "text": "<diff>" } ],
  "_meta": { "moxy/cache": "always" } }
```

Every other subcommand emits no `moxy/cache` (threshold fallback):

```json
{ "content": [ { "type": "text", "text": "M  file.txt" } ] }
```

Valid intents: `"always"`, `"threshold"`, `"never"`. Invalid (→ threshold
fallback, call still succeeds): `"aggressive"`, a number, a missing `_meta`.

### 6. Future extension (informative, non-normative)

A later version MAY define a **per-block** `_meta.moxy/cache` on individual
content blocks, so a tool can cache one block `always` and another `never`
within a single result. This version specifies only the result-level signal; a
result-level intent and a future per-block intent are expected to compose with
the per-block value taking precedence for the block that carries it.

## Security Considerations

The `moxy/cache` intent is data emitted by the moxin tool and read by moxy; it
crosses no new trust boundary beyond the tool's own output, which moxy already
parses in `mcp-result` mode. The intent governs *only* whether output is
blob-cached versus returned inline — never whether a tool runs, what it may
access, or what a client is shown. It MUST NOT be treated as a security control;
authorization remains the exclusive domain of `perms-request` / `[dynamic-perms]`.

Because the intent can be influenced by anything that influences the tool's
output, a tool whose output embeds untrusted data MUST NOT derive the cache
intent from that untrusted portion in a way that could be steered (e.g. echoing
attacker-controlled text into `_meta`). The blast radius is limited — the worst
outcome is a large output cached (or not) against the author's preference — but
authors SHOULD compute the intent from trusted, tool-internal state (such as
which subcommand ran), not from payload content. moxy strips `moxy/cache` before
returning the result, so the signal is not disclosed to clients.

## Conformance Testing

Conformance tests for this specification live in `zz-tests_bats/` (the moxy bats
suite; the `dynamic-cache` cases run under the existing native-server lane).

Tests use binary injection via `bats-emo`:

    require_bin MOXY_BIN moxy

### Covered Requirements

| Requirement | Test File | Description |
|-------------|-----------|-------------|
| §1, MUST require `result-type = "mcp-result"` when `cache-results = "dynamic"` | `dynamic_cache.bats` | Config load errors when `dynamic` is set under `text` mode |
| §1, MUST reject an invalid `cache-results` value | `dynamic_cache.bats` | Config load errors on e.g. `cache-results = "sometimes"` |
| §2, `moxy/cache` = `always`/`threshold`/`never` selects the mode | `dynamic_cache.bats` | Each value yields the matching cache shape (blob URI vs inline) in the result |
| §2, MUST strip `moxy/cache`/emptied `_meta` before returning | `dynamic_cache.bats` | The returned result carries no `moxy/cache` key |
| §3, MUST fall back to `threshold` on absent/malformed intent | `dynamic_cache.bats` | A result with no `_meta`, and one with an unrecognized value, both yield threshold-shaped results and succeed |

## Compatibility

Backwards compatible. Tools that omit `cache-results` or set it to `always` /
`threshold` / `never` are unaffected; the static resolution path is unchanged.
`dynamic` is a new opt-in value used by no existing moxin config, and it is
inert unless paired with `result-type = "mcp-result"` (enforced at load).
Results that carry no `moxy/cache` behave exactly as a `threshold` tool would.
A moxy predating this RFC treats `cache-results = "dynamic"` as an invalid value
and rejects the config at load, so a downgrade fails loudly rather than silently
mis-caching. The `moxy/cache` key is namespaced to avoid collision with other
`_meta` conventions.

A known limitation, inherited from the `mcp-result` caching path (#319): a
resolved `always` does not cache a text block that carries no `mimeType`, because
that path only elevates mime-bearing blocks to cached resources. Honoring
`always` for mime-less blocks (for both the static and dynamic policies) is a
forward-compatible follow-up that would broaden, never narrow, what is cached; no
config written against this version would break under it.

## References

### Normative

- [RFC 2119] — Key words for use in RFCs to Indicate Requirement Levels.

### Informative

- FDR 0012 (`docs/features/0012-grit-capability-families.md`) — the
  capability-family design whose split `diff` motivates this mechanism.
- moxin(7) RESULT SHAPING — `result-type` (`mcp-result` vs `text`) and
  `cache-results` (`always`/`threshold`/`never`, #319), the envelope-ownership
  and static caching this specification builds on.
- The `perms-request = "dynamic"` / `[dynamic-perms]` mechanism
  (`internal/native/dynperms.go`) — the predicate-based alternative considered
  and not adopted, for the reasons in the Introduction.
