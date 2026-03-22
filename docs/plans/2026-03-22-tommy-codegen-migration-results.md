# Tommy Codegen Migration Results --- Moxy

**Date:** 2026-03-22 **Repo:** amarbel-llc/moxy (issue #8) **Tommy version:**
v0.0.0-20260322231344-5bfc25360cab

## Summary

Migrated moxy's config package from hand-rolled CST parsing and
marshal-shim-based serialization to tommy's generated codegen
(`//go:generate tommy generate`). The migration deleted \~100 lines of manual
code and replaced it with a single generated file.

## What worked well

- **Zero caller changes.** We used approach B: `Parse` calls `DecodeConfig`
  internally and returns `Config`. No signature changes propagated to `Load`,
  `LoadHierarchy`, `Merge`, `validate`, or `main`. This is the recommended
  approach for downstream consumers that only need read access.

- **Custom marshal types.** `Command` with `UnmarshalTOML`/`MarshalTOML` worked
  correctly. The codegen calls `UnmarshalTOML` via `GetRawFromContainer` on
  decode and `MarshalTOML` on encode. One requirement: the type must have
  `MarshalTOML() (string, error)` for encode support --- moxy only had
  `UnmarshalTOML` and had to add `MarshalTOML`.

- **Pointer-to-primitive fields.** `GenerateResourceTools *bool` with
  `toml:"generate-resource-tools"` decoded and encoded correctly, including the
  nil-vs-present distinction.

- **Zero-value skip.** `Paginate bool` is only written when non-zero or already
  present in the document, avoiding polluting moxyfiles with `paginate = false`.

- **Array-of-tables append.** `WriteServer` appends new `[[servers]]` entries
  via `doc.Data().Servers = append(...)` followed by `doc.Encode()`. Comments
  and formatting in existing entries are preserved.

- **Round-trip fidelity.** No-op decode→encode preserves byte-identical output
  including comments and whitespace.

## Issues encountered

### 1. Flat keys for pointer-to-struct fields (tommy#12, fixed)

**Severity:** Blocker --- blocked the migration until fixed.

Moxy's `Annotations *AnnotationFilter` uses `toml:"annotations"`, but the
existing moxyfile format puts annotation keys flat in the server table:

``` toml
[[servers]]
name = "lux"
command = "lux"
readOnlyHint = true
```

The initial codegen only looked for a `[servers.annotations]` sub-table via
`FindTableInContainer`. The fix added a flat-key fallback: when the sub-table is
not found, the codegen checks for each field directly in the parent container.

**Recommendation for other consumers:** If you have pointer-to-struct fields
where the TOML format uses flat keys (no sub-table), verify this works with the
current codegen. The sub-table form should also be tested.

### 2. MarshalTOML required for encode (expected, not a bug)

Types with `UnmarshalTOML` must also have `MarshalTOML` for `Encode()` to work.
This wasn't documented --- we discovered it when the generated code referenced
`MarshalTOML` and it didn't exist.

**Recommendation:** Tommy's docs or `generate` command should warn (or error at
generation time) when a type has `UnmarshalTOML` but no `MarshalTOML`.

### 3. gomod2nix.toml staleness (tommy repo, fixed)

The initial tommy release with codegen had a stale `gomod2nix.toml` --- the new
`golang.org/x/tools` dependency wasn't reflected. This caused Nix build failures
when consuming tommy as a devshell input.

**Recommendation:** CI should verify `gomod2nix.toml` is up to date after
dependency changes. This is a general gomod2nix hygiene issue, not
tommy-specific.

## Migration pattern for other consumers

1.  **Add `MarshalTOML`** to any type that has `UnmarshalTOML`
2.  **Add `//go:generate tommy generate`** above the root config struct
3.  **Run `go generate`** to produce the companion `_tommy.go` file
4.  **Choose approach A or B:**
    - **B (recommended):** Keep existing `Parse` signature, call `DecodeConfig`
      internally, return `doc.Data()`. Minimizes blast radius.
    - **A (if needed):** Return `*ConfigDocument` from `Parse` for callers that
      need comment-preserving round-trips.
5.  **Replace marshal shim** code with `DecodeConfig`/`Encode` directly
6.  **Check for flat-key struct fields** --- test both flat and sub-table forms
7.  **Commit the generated file** --- standard Go convention for `go:generate`

## Test coverage

After migration: 82.1% statement coverage on the config package (unit tests).
Bats integration tests additionally cover the read path end-to-end through the
real binary.

New tests added to validate no regressions: - Annotations: sub-table form,
multiple flat keys, absent annotations - All fields combined (name, command,
paginate, generate-resource-tools, annotations) - Round-trip:
decode→encode→re-decode with all field types - WriteServer with paginate and
generate-resource-tools

## Deleted code

- `parseCommandFromNode` --- manual CST command extraction
- `parseAnnotations` --- manual CST annotation extraction
- `moxyfileConfig` / `moxyfileServer` --- intermediate marshal-compatible
  structs
- `toMoxyfileServer` --- conversion function
- All `tommy/pkg/marshal` imports
- All `tommy/pkg/cst` and `tommy/pkg/document` imports (moved to generated file)
