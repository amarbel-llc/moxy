---
status: experimental
date: 2026-03-19
decision-makers: Sasha F
---

# Use tommy for comment-preserving moxyfile edits

## Context and Problem Statement

The `moxy add` command appends new `[[servers]]` entries to a moxyfile by
concatenating TOML text. This approach cannot update an existing server in place
and would clobber user comments if it ever needed to rewrite the file. How should
moxy parse and modify moxyfiles so that comments and formatting survive
round-trip edits?

## Decision Drivers

* User comments in moxyfiles must survive `moxy add` and future edit commands
* The `add` command should update in place when a server name already exists
  instead of appending a duplicate
* Moxy already uses TOML `[[servers]]` array-of-tables, so the parser must
  handle that syntax
* Prefer an in-house library (tommy) over a third-party dependency for
  long-term control and consistency across the toolchain

## Considered Options

* **Option 1: tommy (CST-backed TOML library)** — use the marshal API
  (`UnmarshalDocument` / `MarshalDocument`) for struct-based round-trip editing
  with comment preservation
* **Option 2: BurntSushi/toml + string append (status quo)** — keep the current
  `BurntSushi/toml` parser for reading and raw string concatenation for writing
* **Option 3: pelletier/go-toml v2** — third-party library with some
  comment-preservation support

## Pros and Cons of the Options

### Option 1: tommy

* Good, because CST design preserves comments, whitespace, and formatting
  byte-for-byte for untouched regions
* Good, because the marshal API maps directly to moxy's existing `Config` /
  `ServerConfig` structs via `toml:` tags
* Good, because it is an in-house library — bugs and missing features can be
  fixed on our timeline
* Neutral, because tommy is newer and less battle-tested than BurntSushi/toml

### Option 2: BurntSushi/toml + string append (status quo)

* Good, because it is already implemented and working for the simple append case
* Good, because BurntSushi/toml is mature and well-tested
* Bad, because it cannot update a server entry in place without rewriting the
  entire file and losing comments
* Bad, because appending blindly creates duplicates when a server name already
  exists

### Option 3: pelletier/go-toml v2

* Good, because it has partial comment-preservation support
* Bad, because comment preservation is incomplete and has known edge cases
* Bad, because it adds an external dependency with no control over fix timelines
* Bad, because it does not align with the toolchain's direction toward tommy

## Decision Outcome

Chosen option: "tommy", because it preserves comments and formatting byte-for-byte
during round-trip edits, maps directly to moxy's existing structs via `toml:` tags,
and the `[[array-of-tables]]` blocker (tommy#5) has been resolved upstream.

### Consequences

* Good, because `moxy add` can now update servers in place without clobbering comments
* Good, because the same parse-modify-write pattern extends to future edit commands
* Good, because `BurntSushi/toml` can be removed as a dependency
* Bad, because moxy takes a dependency on a younger library with a smaller user base

### Confirmation

Verified with an evaluation test against tommy@ff251e0cfdc0:
- No-op round-trip produces byte-for-byte identical output
- Appending a `[[servers]]` entry preserves all existing comments
- Updating a field within an existing entry preserves inline and top-level comments

## More Information

* Upstream issue (resolved): [amarbel-llc/tommy#5 — Support \[\[array-of-tables\]\] in
  document and marshal APIs](https://github.com/amarbel-llc/tommy/issues/5)
* Moxy moxyfile hierarchy design:
  `docs/plans/2026-03-18-moxyfile-hierarchy-design.md`
