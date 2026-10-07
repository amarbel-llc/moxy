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
| `allow-tools` | If P sets it, C sets it and every C entry is covered by a P entry. |
| `resolved-moxins` (record) | Every moxin C resolves, and can reach, resolves to the same directory in P's record. Only when both sides carry a record. |
| `ephemeral`, `progressive-disclosure`, `paginate`, `generate-resource-tools` | Presentation, not rights; ignored. |

### `allow-tools`

A new top-level moxyfile key, the allowlist the disable lists are not:

```toml
allow-tools = ["smith", "grit.status", "restart"]
```

A bare name is a whole server or moxin, or one of moxy's meta tools; a dotted
pair is one tool. Anything not named is neither listed nor callable — meta
tools and `[[servers]]` tools included. Unset allows everything; `[]` allows
nothing. Across the cascade the lists **intersect**, so every layer that sets
the key can only shorten it. Enforced in the proxy at `tools/list` and at the
single dispatch funnel, beside `--expose` and the dynamic exclude set.

`disable-moxins` fails open: a moxin that grows a tool exposes it until
someone adds a disable. `allow-tools` fails closed, which is what makes
"anything not listed does not exist" a property of moxy rather than of the
launcher's brief.

### `resolved-moxins`: moxin identity

Permission tiers are not in the moxyfile. Each moxin tool declares its own
`perms-request`, read from the directory the moxin was discovered in, and
`MOXIN_PATH` decides which directory wins. So the one way a child can widen a
tier — or gain a moxin outright — is a different `MOXIN_PATH`, and no moxyfile
key can express or forbid that.

`show-effective-moxyfile`, when it renders a directory, therefore records what
discovery found there and then:

```toml
[resolved-moxins]
"grit" = "/nix/store/…-moxy-moxins/share/moxy/moxins/grit"
"notes" = "/home/u/.config/moxy/moxins/notes"
```

(winning directory per name, symlinks resolved). The key is a record, not
configuration: a serving moxy never reads it from its own moxyfile, only from
the parent reference. At serve time moxy substitutes its *own* discovery for
the child side and requires every moxin it can reach to be in the parent's
record, from the same directory. An empty table is a record of no moxins, not
an absent record.

### `moxy narrows --parent <ref> --child <ref> [--json]`

Each `<ref>` is a directory (walked as if it were the working directory:
global moxyfile, `$HOME`→dir, includes) or a file (that file plus its
includes, nothing else).

- Exit `0`: the child narrows the parent; nothing is printed.
- Exit `1`: it does not. One line per violation on stdout, `<key>: <reason>`,
  sorted by key, then subject, then reason — the same two configs always
  produce the same bytes, so a caller can store the lines verbatim.
- Exit `2`: a side could not be read or parsed, or the arguments were
  malformed; the reason is on stderr. Exit `1` is never anything but the
  verdict.
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
- Set but unreadable, unparseable or empty: refusal (fail closed). An empty
  file would parse as a parent that constrains almost nothing, and is far
  likelier a render that failed.
- Read once, at startup, and pinned for the life of the process. A reload is
  judged against that reading, so a child that can write the file cannot
  loosen it and then call `restart`.
- A restriction key written with the wrong type (`allow-tools = "x"`) is a
  parse error on either side, never "unset": unset means unrestricted.
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

- **`moxy narrows` cannot know a future child's `MOXIN_PATH`.** Given a child
  *file* it judges the keys alone (plus whatever record the file carries);
  given a child *directory* it discovers moxins in the caller's environment,
  which matches serving only if the child is started with the same
  `MOXIN_PATH`. The `resolved-moxins` rule is certain only when the child
  serves. A launcher that wants the early answer to hold pins the child's
  `MOXIN_PATH` to the parent's; the serve-time check is the backstop, not a
  substitute.
- **A hand-written parent reference has no record**, and then moxins are not
  compared at all. The guarantee needs a reference written by
  `show-effective-moxyfile` from a directory.
- **The record names directories, not contents.** For store paths that is the
  same thing. A mutable moxin directory the child can write to can still be
  edited after the fact; keeping the child out of it is the sandbox's job.
- **A different moxy build is a different system moxin directory**, so a child
  served by another build than the one that rendered the reference is refused
  while the system moxins are on. That is the conservative answer — another
  build may ship other tiers — but it means parent and child must run the
  same moxy.
- **Serve flags are outside the moxyfile.** `--expose` and `--name-template`
  belong to the child's command line.
- **`moxy serve-moxin` reads no moxyfile** and so is not subject to this
  check. A launcher that must enforce narrowing runs `serve mcp` or
  `serve-http`.
- **`[credentials]` may be omitted by the child**, which falls back to the
  system keychain. The tokens involved are for `url` servers, and those must
  match the parent's exactly.
- **`allow-tools` governs tools only.** Resources and prompts of a server
  whose tools are all unlisted still flow, and the `moxy://tools/…` discovery
  resources still describe unlisted tools. Same boundary as `--expose` and the
  exclude endpoint.
- **Bare names are one namespace.** `allow-tools = ["restart"]` allows the
  meta tool and would also allow a server someone named `restart`.
- **No signature.** The parent reference is trusted as given. Whoever sets
  `MOXY_PARENT_MOXYFILE` vouches for the file.
