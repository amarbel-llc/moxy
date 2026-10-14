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
| relative paths | A `command`, `headers-helper`, `[credentials]` command or `nix-devshell` flake ref (`.`, `a/b`, `path:./x`, `git+file:.`) relative to the working directory is refused in C even when identical to P's. Bare PATH names, `~/`, `$VAR` and registry/remote flake refs pass; resolving those is the launcher's env. |
| `[servers.annotations]` | The filter is OR over its hints, so each hint adds tools. If P filters, C filters too and every hint C sets is one P sets with the same value (subset); C may set fewer. List-only: see Limitations. |
| `disable-moxins` | C covers every P entry; a whole-moxin entry covers a single-tool entry, not the reverse. Also: a server name P defines and C does not must be in C's `disable-moxins`, because a `[[servers]]` entry shadows the same-named moxin. |
| `builtin-native` | If P is `false`, C is `false`. (The merge is closest-wins here; narrowing is not.) |
| `[credentials]` | If C sets it, it equals P's. |
| `allow-tools`, `pass-tools` | If P sets `allow-tools`, C sets it, and every entry of C's `allow-tools` and `pass-tools` is covered by an entry of P's `allow-tools` or `pass-tools` (moxy#459). |
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

### `pass-tools`: confer, never call (moxy#459)

Plain subset narrowing cannot express a supervisor that may hand a tool to
its children but must never call it: to confer `create_issue` it would have to
hold it in `allow-tools`, making it the widest principal in its graph (circus
FDR-0041 §P9; the AWS IAM PassRole shape). So a file holds every tool at one
of three levels, none < pass < use, and a second key names the pass ones:

```toml
allow-tools = ["grit.status"]
pass-tools  = ["smith.issue-create"]
```

- **Serving** reads `allow-tools` only. A pass tool is neither listed nor
  callable by its holder.
- **Narrowing** compares levels: every tool C holds, at use or at pass, must be
  held by P at use or at pass. A child may therefore *use* what its parent only
  *passes* — the one upward step, spinclass FDR 0032 D11's escalation,
  pre-approved when P's moxyfile was written. moxy enforces the relation; it
  does not judge whether that approval happened.
- **The hierarchy merge** keeps, per tool, the lowest level any layer setting
  `allow-tools` holds it at. A closer layer can lower use to pass or pass to
  none, never raise; a layer without `allow-tools` changes nothing.
- **Refused at load:** `pass-tools` without `allow-tools` (with no allowlist
  every tool is usable, so a pass tool would be callable), and a `pass-tools`
  entry `allow-tools` already covers (one tool at two levels; "use wins" would
  let the holder call what the file said it must not, "pass wins" would make
  something other than `allow-tools` filter the serve surface). A bare pass
  name over a dotted allowed pair is fine: `allow-tools = ["grit.status"]`,
  `pass-tools = ["grit"]` uses one tool and may confer the rest.

**Pass bounds the served surface, not what a holder can become.** A reload
(`restart`) is judged only against the parent reference pinned at startup,
and that reference permits a child to hold at use what the parent passes. A
session whose `MOXY_PARENT_MOXYFILE` holds X at pass, and that can rewrite its
own moxyfile and call `restart`, can move X into `allow-tools` and call it: it
is bounded only by its reference, under which pass may become use. Where that
matters, keep `restart` out of `allow-tools`, or the moxyfile unwritable by
the session. Judging reloads against the startup config instead would also
block deliberate operator widening; that is a policy question tracked
separately, and the reload code is unchanged here.

Because a parent with `pass-tools` always has an allowlist, the existing rule
"a child may omit `allow-tools` only when P sets none" means such a parent's
child always sets `allow-tools`, if only `[]` beside the tools it passes on.

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
- The path must be absolute.
- Set but relative, unreadable, unparseable or empty: refusal (fail closed). An empty
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
- **Annotation filters are not a boundary.** moxy applies
  `[servers.annotations]` to `tools/list` only; nothing checks it at call
  time, so a client that knows a hidden tool's name can call it. Narrowing
  keeps a child from *listing* more than its parent, no more. `allow-tools`
  is the boundary. Enforcing the filter at dispatch is a separate change.
- **A hand-written parent reference has no record.** A serving moxy reads
  that as a record of no moxins and refuses any moxin it can reach (the
  default `MOXIN_PATH` includes the working directory's `.moxy/moxins`, so
  "unchecked" would be a hole). `moxy narrows` skips the moxin rule when
  either document lacks a record. Write the reference with
  `show-effective-moxyfile` from a directory.
- **Write the reference atomically.** A truncated file that still parses is a
  looser reference than intended; there is no end marker.
- **Arguments are not inspected.** A relative path among a command's
  arguments still resolves against the child's working directory; only the
  executable and `nix-devshell` are checked.
- **Stricter parsing is not backward compatible.** `allow-tools`,
  `disable-moxins`, `disable-servers`, `builtin-native` and `resolved-moxins`
  written with the wrong type used to be ignored and are now a load error
  everywhere (serve, status, reload); a `disable-moxins` entry with a leading
  dot is likewise rejected. A moxyfile that loaded before may now refuse to.
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
