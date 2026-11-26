# Splitting moxy's tool engine out for spinclass: plan

Date: 2026-11-26. Status: **proposal, no code**. Where the engine lives is the
operator's call. Labels: **R** read in code or docs, **T** tried live,
**U** not verified.

## Short answer

- **No big split is needed.** moxy is already a Go library: `pkg/moxy`,
  `pkg/perms`, `pkg/policybroker`, and a `go-pkgs` flake output (R, FDR 0014).
  spinclass's flake does not mention moxy (R). No consumer's build is verified
  (U).
- **Linking the library does not give the folio tools.** They are TOML files
  plus shell scripts that nix wraps, not Go.
- **Today's tools cannot be held to one directory.** Two ways out were
  reproduced.
- **Recommendation:** the engine stays in moxy and spinclass imports it. Build
  the first slice as a small new Go package of four file tools.

## 1. The engine today

| Part | Code (R) | Does spinclass need it? |
|---|---|---|
| Tool definitions | `internal/native` (TOML schema, discovery); `moxins/folio`, `rg`, `grit` | yes |
| Permissions | `pkg/perms`, `pkg/policybroker`; floor script `folio-perms` | yes |
| Running a tool | `internal/native` (processes, blob inputs, output cache, madder) | yes |
| Safe opening | `internal/readguard`, `cmd/readguard`; `pkg/toolbox` | the idea; not enough as is |
| Facade | `pkg/moxy` (Open, Tools, Call, Caller, Result) | for existing moxins |
| Proxy around it | `internal/proxy` (`proxy.go` alone is 2673 lines), `mcpclient`, moxyfile, narrowing, credentials, OAuth, async | no, but `pkg/moxy` pulls it in |

Dependencies: `pkg/perms` uses `internal/native` and `pkg/toolbox`.
`internal/native` links madder in process. `pkg/moxy` uses nearly everything.
Go allows an outside module to import `pkg/` even though it uses `internal/`.

## 2. Why today's tools cannot be scoped to one directory

The live tries used the moxy serving the planning session (0.6.46+7bfe461).

1. **T** `folio.glob` checks `path` only. `pattern="/etc/os-releas*"` returned
   `/etc/os-release`. The gap is noted in
   `2026-11-10-489-field-typing-table.md`; it is not fixed.
2. **T** `folio.ls` passes `flags` unquoted to `ls`. `path="."`,
   `flags="-ld /etc/os-release"` listed that file. **R, not run:** `du` has the
   same shape; `tree` has `follow_symlinks`.
3. **R** The floor is much wider than one directory: working directory,
   `/nix/store`, `~/.claude`, temp and log directories, sibling repos. A broker
   that does not answer falls back to it.
4. **R** `readguard` asks the broker named by the environment, else the floor.
   It never sees an in-process `Options.Gate`. By the code (not tested), a link
   inside a narrow scope that points at a floor-allowed place would be followed.
5. **R** Under the default `delegate` mode with no hook, the server does not
   enforce "ask": its call path refuses only a broker deny.
6. **R** `folio.read` and `rg.search` return large output whole
   (`no-truncate`), and process output is buffered without a limit.

No existing issue for 1 or 2 turned up in title searches (U: not exhaustive).
**None is filed or fixed by this plan.** This file describes unfixed holes;
decide on that before it is pushed.

## 3. Where the engine should live

| Home | For | Against |
|---|---|---|
| **moxy (recommended)** | code, tests and producer output are there; standalone moxy and the krone origin need no new dependency | spinclass inherits moxy's flake inputs (madder, tommy, go-mcp, clown) |
| clown (spinclass#438 ruling 9) | one home for clown and juggler | moves all the code first; moxy and spinclass both wait on clown |
| spinclass | next to the session policy | standalone moxy would depend on a session manager |

This conflicts with two records that need a ruling, then correction: ruling 9
(clown exports the engine) and circus EPIC 0001 decision 17 (clown runs the
moxy library as the frontend proxy). Staying in moxy keeps decision 17
possible: both could import it.

```
        juggler run                 confined session        standalone moxy
             |                             |                 (and krone)
             v                             v                       |
   +---------------------- spinclass serves ------------+          |
   | who is calling -> scope: one root, plus its policy |          |
   +--------+-----------------------------+-------------+          |
            |                             |                        |
  ==========|=============================|========================|== moxy repo
            v                             v                        v
   pkg/filetools (NEW, slice 1)     pkg/moxy ----------------- cmd/moxy
   read, list, glob, grep           moxins, perms, cache,      proxy, hook,
   beneath ONE root fd              running tools              transports
            ^                             |
            +--- later: folio/rg moxins run a binary over this package
```

## 4. Smallest boundary, and what stays in moxy

- **Slice 1** needs one new public package using only the standard library and
  a system-call package, plus a pinned `rg`. It needs no `pkg/moxy`, moxyfile,
  madder or broker.
- **Everything else** (other moxins, grit): use `pkg/moxy` as it is. Carve a
  smaller `pkg/engine` out of it only if its dependency weight is measured and
  hurts.
- **Stays CLI-only:** transports, the Claude Code hook, `status`/`add`/`narrows`
  and the other subcommands, environment reading, statsd and log setup.

What an importer of `pkg/moxy` must still handle:

| Area | Effect |
|---|---|
| Moxin definitions | The build default for the moxin directory is set by moxy's linker flags, so pass `SystemMoxinDir` pointing at the `moxy-moxins` nix output. It holds every shipped moxin; narrow with `allow-tools`. Version and ringmaster defaults are also missing |
| Result cache | `Open` wants a madder store unless `Options.Blobs` is given. The cache threshold is fixed at about 200 tokens. spinclass's madder pin must read the host's stores |
| Output limits | There is no hard cap or sync-call timeout. Both are new work |
| Blob inputs | A digest names any blob in the store. A scoped caller must be limited to blobs it produced, or get none |
| Broker | RFC 0002 is unchanged for standalone moxy, the hook and `readguard`. Inside spinclass, `Options.Gate` can answer in process, but see hole 4 |
| Per-caller scope | `Caller` carries only an id, a cwd and a tighter policy. The gate gets the call's context, but no contract says who is calling. One instance per scope works today; a `Caller.Scope` field is later work |

**Rule for scope:** it is a hard limit checked first, with **no floor and no
fallback**. A broker or policy may only narrow it further (principle 4).

## 5. First slice: four file tools beneath one directory

`pkg/filetools`: open a root once, then serve `read` (line range), `list`,
`glob` and `grep`, with their tool schemas.

- Every open is `openat2` with `RESOLVE_BENEATH` from the root. Relative links
  that stay inside work; absolute or escaping links are refused. Linux only;
  other systems fail closed.
- No shell and no free-form flags. `glob` matches inside the root only.
- `grep` runs `rg` from the root's descriptor, with no config file, no
  parent or global ignore files, no link following, and the pattern after `--`.
  `rg` opens by name, so a tree being **written during the run** has a race
  window. Fine for a fixed review copy (see question 5).
- Hard caps on bytes returned and on time, with paging by offset. No blobs.
- "Outside" and "missing" give the same error.
- Tests first, reusing the cases circus's review tools hit
  (`2026-11-26-first-review-build.md`): `..`, links out, a file name ending in
  a newline, error messages that reveal outside files, runaway searches.

It does not replace the moxins yet, so for a while there are two
implementations. The end state is one: `folio.read`/`ls`/`glob` and
`rg.search` run a small binary over this package, as `toolbox` already does.

This slice is **not a sandbox**. It limits what these tools open, nothing else.

## 6. Can a juggler run start a spinclass MCP server from its moxyfile?

**From moxy's side, probably yes; nothing was run.**

- **R** A `[[servers]]` entry with `command` starts any stdio MCP server. No
  environment override was found in the spawn code, so it should inherit
  moxy's.
- **R** With a parent reference set, the child **may not add or change** a
  server; `command` is compared argument by argument. So the parent must list
  it, and a per-run root in the arguments would be refused. Pass the root in the
  environment, set by the launcher, never the model. The server must refuse to
  start without it.
- **R** The brief's `allow-tools` must list the tools. Under `enforce`, a child
  server's tools also need `enforce-allow`.
- **R (circus record, not rechecked)** `juggler run` runs moxy with a moxin
  given by path, needed a madder store, and did not pass the brief's `[env]`
  to tools; the circus recipe set the environment itself.
- **U** Whether a juggler brief accepts `[[servers]]` (juggler's session).
  Whether spinclass can serve without a spinclass session (spinclass's).
- **Cheap proof:** one fake-model run whose brief names a stub server and
  checks which tools are offered.

## 7. Steps, in order

Sizes are rough guesses, in changed lines including tests.

| # | Step | Where | Size |
|---|---|---|---|
| 0 | Close holes 1 and 2 (and check `du`) in today's moxins, tests first. Ranked first as security work (principle 26) | moxy | S, <200 |
| 1 | Prove an outside module builds against `go-pkgs` (a throwaway consumer) | moxy, then spinclass | S |
| 2 | Slice 1, `pkg/filetools` | moxy | M, 600 to 1000 |
| 3 | spinclass serves it for a given root; run the juggler proof | spinclass, juggler | not sized |
| 4 | Point the folio and rg moxins at the package; delete the old scripts | moxy | M |
| 5 | Per-caller scope, output caps and blob limits for general moxins in `pkg/moxy` | moxy | L, design first |
| 6 | Sessions get folio through spinclass; then grit (moxy#482, spinclass#379); retire moxy's copies | both | L |

**No rush from review runs:** the circus plan records that today the operator
chose circus's own limited review tools for now, with circus#514 for the later
switch.

Related: moxy#508 (one shared moxy per host) also needs a per-caller identity.
Design step 5 once for both.

## 8. Risks

- **Two scope systems** (root scope and `[folio]` policy) could disagree. Rule:
  both must allow.
- **Two implementations** between steps 2 and 4 could drift. Step 4 is the exit.
- **madder version skew:** an older pin in spinclass fails to open newer stores.
- **Pre-1.0 `pkg/` surface:** a moxy bump can break spinclass's build.
- **Host or unit:** if spinclass runs tools on the host for a confined agent,
  a hole in a tool gets round confinement. A root check alone does not fix that.
- **grit** runs the user's own git and its predicates assume the session
  worktree. Moving it is more than a library import.

## 9. Questions for the operator

| # | Question | Recommended default |
|---|---|---|
| 1 | Where does the engine live? | moxy; correct ruling 9, and check decision 17 against it |
| 2 | Does "folio moves into spinclass" mean spinclass *serves* it, or *owns* the code? | serves it; one copy of the code, in moxy |
| 3 | Fix the two holes now, apart from the split? File them where? | yes, step 0; file on moxy once you accept the repo's visibility |
| 4 | Slice 1: new Go package, or harden the shell scripts? | new Go package |
| 5 | `grep`: `rg` by name, or a slower, race-free Go search? | `rg` while the tree is fixed; revisit before scoping a live worktree |
| 6 | Blobs in a scoped run? | none; capped output with paging |
| 7 | Do spinclass's tools run on the host or in the confined unit? | in the unit; not assumed by slice 1 |
| 8 | Do standalone moxy sessions keep folio after step 6? | yes, from the same package |
