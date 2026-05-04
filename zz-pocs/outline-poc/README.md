# outline-poc

Tree-sitter–based source code outlines for agents.

**Status:** POC — proves the concept; not a moxin yet.

## Hypothesis

A single bun + `web-tree-sitter` driver can produce useful, hierarchical
outlines (with line ranges) across a handful of common languages — enough
to validate the approach before promoting it to a moxin under
`moxins/outline/`.

## Result

Validated, with one caveat. Seven languages work end-to-end against the
WASM grammars shipped in the [`tree-sitter-wasms`][tsw] npm package:

| Language   | Sample              | Status |
| ---------- | ------------------- | ------ |
| Go         | `samples/sample.go` | PASS   |
| TypeScript | `samples/sample.ts` | PASS   |
| JavaScript | `samples/sample.js` | PASS   |
| Rust       | `samples/sample.rs` | PASS   |
| Python     | `samples/sample.py` | PASS   |
| PHP        | `samples/sample.php`| PASS   |
| Bash       | `samples/sample.sh` | PASS   |

**Caveat:** justfile and Nix are out of scope for this POC.
- `tree-sitter-nix` (nix-community) ships parser source but no prebuilt WASM
  on npm; building one requires `tree-sitter-cli`, which the POC didn't take
  on as a dependency.
- `tree-sitter-just` (IndianBoy42) is in the same situation. The grammar
  exists but isn't packaged with WASM artifacts.

If/when this graduates to a moxin, both can be added by compiling
their WASM at flake build time and mounting them next to the
tree-sitter-wasms bundle. Not POC-scope.

## Running

```sh
just zz-pocs/outline-poc/install      # one-time bun install
just zz-pocs/outline-poc/outline-all  # outline every sample
just zz-pocs/outline-poc/outline <path>  # outline a file or directory
```

## Output shape

Hierarchical with line ranges, one symbol per line, two-space indentation
per nesting level:

```
# /path/to/file.go
type Greeter [9-12]
  field Salutation [10-10]
  field count [11-11]
method Greet [15-18]
method Count [20-20]
func NewGreeter [22-24]
type Renamer [30-32]
  method Rename [31-31]
```

Directory mode walks recursively, skipping `.git`, `node_modules`,
`.venv`, `target`, `dist`, `build`, `.tmp`, and `result/`. Files with
unsupported extensions are silently skipped.

## Architecture

- `outline.ts` — main driver. Hardcoded `LANGS` table maps file extension
  to a WASM path and a list of `NodeRule`s. Each rule names a tree-sitter
  node type and how to extract the symbol's name (either via a field name
  or by finding the first descendant of a given type).
- `dump.ts` — debug helper. Prints the raw S-expression for a file in a
  given language. Used to discover node types when authoring rules.
- `probe.ts` — Phase 1 sanity check. Confirms `web-tree-sitter` loads
  and parses Go under bun.
- `samples/` — vendored, hand-authored test inputs per language.

The `findName` helper supports both `{field: "name"}` (uses
`childForFieldName`) and `{child: "type_identifier"}` (DFS for first
descendant of that type). Most rules use the field path; the child
fallback is for nodes like Go's `type_spec` where the name lives on a
nested `type_identifier`.

## Out of scope

Deliberately omitted:

- Tests (no bats wrapper; the recipe self-asserts via PASS/FAIL counts).
- CLI flags or env-var configuration. Hardcoded constants only.
- Any moxin scaffolding (`_moxin.toml`, perms-request, result caching).
- Symbol search across files. The original scope (option (c)) was
  discarded in the question phase in favor of "outline + directory walk".
- Justfile and Nix grammars (see Caveat above).

## Notes for graduation

If this becomes a moxin, the to-do list is roughly:

1. Replace `samples/` with golden-output regression tests under
   `zz-tests_bats/` once the moxin is wired into the nix build.
2. Move bun + zx scripts under `moxins/outline/src/`, follow the
   `mkBunMoxin` pattern, and keep WASM grammars in the moxin's
   nix derivation rather than `node_modules/`.
3. Add justfile and nix grammars by compiling their WASM at build time.
4. Decide whether to expose two tools (`outline`, `outline-tree`) or one
   that switches on `statSync`.
5. Annotate rules with the grammar version they were authored against;
   tree-sitter grammars rename node types between releases (saw this with
   Go's `method_spec` → `method_elem`).

[tsw]: https://www.npmjs.com/package/tree-sitter-wasms
