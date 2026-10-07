---
status: proposed
date: 2026-10-07
promotion-criteria: clown's juggler calls `moxy narrows` before minting a
  child and sets `MOXY_PARENT_MOXYFILE` in the agent unit (clown#248); a
  headless agent whose brief widens its spawner's toolset is refused at both
  layers in a live fleet, and one whose brief narrows it runs
---

# Moxyfile narrowing (`moxy narrows`, `MOXY_PARENT_MOXYFILE`)

Parents: clown FDR 0019 §5 ("Tool plane — moxy, narrowed per agent");
spinclass FDR 0032 D13 (a child's ambient rights are a subset of its
parent's).

## Problem Statement

clown's juggler launches headless agents whose toolset is a moxyfile carried
inline in the agent's brief. The rights model those agents run under says a
child may hold no more than its spawner. Nothing checked that for the tool
plane: juggler trusted the brief.

moxy's own cascade is the wrong operation to lean on. The hierarchy merge
(`internal/config.Merge`) exists so a closer moxyfile can *add* to a farther
one — append a server, replace a server's command, flip `builtin-native` back
to `true`. A child moxyfile that is merged over its parent's can therefore
grant anything. What is needed is a second relation over the same data: not
"merge C over P" but "does C grant no more than P".

"Narrower than" is a statement about moxy's keys and their meaning, so the
relation is defined and enforced here rather than in the launcher.

## Interface

### The relation

`C narrows P` is defined on *effective* configs — each side fully merged, with
includes resolved — and on their literal text: `$VAR` references in `command`
and `headers` are compared unexpanded and the environment is never read.
moxyfile(5) NARROWING is the normative, key-by-key statement. In brief:

| Key | Rule |
| --- | --- |
| `[[servers]]` | Every server C runs (defined and not in C's `disable-servers`) is one P runs, with identical `command` (argv-exact), `url`, `headers`, `headers-helper`, `oauth`, `nix-devshell`. C may omit servers. |
| `[servers.annotations]` | A restriction: C keeps every hint P sets, same value; may set more. |
| `disable-moxins` | C covers every P entry; a whole-moxin entry covers a single-tool entry, not the reverse. Also: a server name P defines and C does not must be in C's `disable-moxins`, because a `[[servers]]` entry shadows the same-named moxin. |
| `builtin-native` | If P is `false`, C is `false`. (The merge is closest-wins here; narrowing is not.) |
| `[credentials]` | If C sets it, it equals P's. |
| `ephemeral`, `progressive-disclosure`, `paginate`, `generate-resource-tools` | Presentation, not rights; ignored. |

### `moxy narrows --parent <ref> --child <ref> [--json]`

Each `<ref>` is a directory (walked as if it were the working directory:
global moxyfile, `$HOME`→dir, includes) or a file (that file plus its
includes, nothing else).

- Exit `0`: the child narrows the parent; nothing is printed.
- Exit `1`: it does not. One line per violation on stdout, `<key>: <reason>`,
  sorted by key, then subject, then reason — the same two configs always
  produce the same bytes, so a caller can store the lines verbatim.
- Exit `2`: a side could not be read or parsed; the reason is on stderr.
- `--json` prints one object instead of lines:
  `{"narrows":bool,"violations":[{"key":"servers.smith","subject":"smith","message":"not in parent"}]}`.
  `violations` is always an array.

Example lines:

```
builtin-native: parent sets false, child does not
disable-moxins: parent disables chix, child does not
servers.extra: not in parent
servers.srv: command differs from parent
```

### `moxy show-effective-moxyfile [--dir <dir>]`

Prints the effective moxyfile for the working directory (or `--dir`) as one
canonical, include-free moxyfile: fixed key order, sorted lists and servers,
commands in array form. Deterministic bytes. Nothing is expanded, so the
output never contains a secret that a `headers` value referred to by `$VAR`.
The output is itself a moxyfile — its own fixed point under this verb — which
is why `narrows` needs no separate "already rendered" flag: `--parent
<rendered file>` means exactly that.

### `MOXY_PARENT_MOXYFILE`

When set, `moxy serve mcp` and `moxy serve-http` load the named file as the
parent reference and refuse to serve unless their own effective moxyfile
narrows it: one line per violation on stderr, non-zero exit, no child server
spawned, no port bound.

- Unset: no check. Existing deployments are unaffected.
- Set but unreadable or unparseable: refusal (fail closed).
- The check is in the config-loading path every serve path shares, not at
  process start only. `restart` with no argument re-reads the hierarchy; a
  moxyfile edited to grant more is refused there too — the restart tool's
  result carries the violations, moxy logs the refusal, and the running
  configuration is left as it was.
- The config is read once, judged in its literal form, and then has its header
  `$VAR`s expanded. What is served is what was judged.

## Rationale

- **A relation, not a merge mode.** Teaching `Merge` a "narrowing only" mode
  would have had to decide what to do with a widening layer — drop it, clamp
  it — and either answer hides the fact from the launcher. A relation with a
  list of violations lets the launcher refuse, name the reason, and record it.
- **Compare active servers, not `disable-servers` sets.** "C's disables ⊇ P's"
  would force a child to list servers it never defines. What matters is what
  runs.
- **Compare literals.** Expanding `$VAR` before comparing would make the answer
  depend on the checking process's environment and would put secrets in the
  parent reference. The launcher owns the child's environment; moxy owns the
  text.
- **The render needed its own writer.** The moxyfile codec marshals a command
  as a space-joined string, which re-splits an argument containing a space, and
  `Parse` expands header values in place. A reference that must round-trip and
  must not leak could use neither.
- **Env, not a flag.** `serve mcp` takes no flags, and the launcher already
  writes the unit environment.

## Limitations

- **`MOXIN_PATH` is outside the moxyfile.** A moxin's tools and their
  permission tiers (`perms-request`) come from whichever directory wins moxin
  discovery. A child with a different `MOXIN_PATH` can reach a same-named
  moxin with looser tiers while its moxyfile narrows perfectly. `moxy narrows`
  cannot see this: it judges a file for a process that does not exist yet. The
  launcher must pin the child's `MOXIN_PATH` to the parent's.
- **Serve flags are outside the moxyfile.** `--expose` and `--name-template`
  belong to the child's command line.
- **`moxy serve-moxin` reads no moxyfile** and so is not subject to this
  check. A launcher that must enforce narrowing runs `serve mcp` or
  `serve-http`.
- **`[credentials]` may be omitted by the child**, which falls back to the
  system keychain. The tokens involved are for `url` servers, and those must
  match the parent's exactly.
- **No signature.** The parent reference is trusted as given. Whoever sets
  `MOXY_PARENT_MOXYFILE` vouches for the file.
