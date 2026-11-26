# Splitting moxy's tool engine out for spinclass: plan

Date: 2026-11-26. Status: **plan, revised the same day for the operator's
rulings**. Only step 0 has code. Labels: **R** read in code or docs, **T** tried,
**U** not verified.

> **ON HOLD: do not act on this plan's ownership parts.** Later on 2026-11-26
> the operator replaced ruling 3 below (as relayed by
> `spinclass/slim-myrtle`): "actually, wait one second. moxy should provide the
> tools that can read/write/manipulate, but spinclass should provide the
> _resources_ that are read / written manipulated. so rg.search stays in moxy,
> and we need to figure out how to make it possibly for moxy's tools to operate
> against the resrouces that spinclass will provide for folio -- yes this is a
> pivot". Every claim that rests on spinclass owning the folio tools, or on moxy
> losing them, is not current: that covers parts of sections 3 to 12. The new
> shape is still being worked out and is not designed here. Sections 1 and 2
> describe the code and stand.

## Rulings (operator, 2026-11-26)

Relayed by chat from `spinclass/slim-myrtle`, who put an earlier version of this
plan to the operator in a decisions document (U: I have not seen it). Quoted
parts are as relayed.

1. File the holes and fix them now. An earlier hold on pushing, because this
   plan describes the holes, was lifted later the same day (as relayed): "I'm
   unconcerned about the holes being public, we can operate normally (not in CVE
   mode)". The lift asks for no merge or deploy; moxy's usual review and merge
   rules apply.
2. Engine home: "moxy for now. we can examine moving to clown in the future
   (we'd move all of moxy into clown)".
3. spinclass **owns** the folio code, "and moxy loses these tools. moxy should
   become something that is purely a library over time, and only clown and
   spinclass use it. moxyfiles would then go away entirely. spinclass is
   responsible for providing agents with views into file systems or file
   system-like providers (folio might support different backends)".
4. "for now to make things easier, inside the session. but eventually, I want
   this outside and I also want to decide if the folio tools should move to a
   cutting-garden plugin that spinclass implements ajour for. also, juggler
   runs should actually be INSIDE the confined unit, and they should get a
   subset of the spinclass-available tools".

## 1. The engine today

| Part | Code (R) |
|---|---|
| Tool definitions | `internal/native` (TOML schema, discovery); `moxins/` |
| Permissions | `pkg/perms`, `pkg/policybroker`; the floor script `folio-perms` |
| Running a tool | `internal/native` (processes, blob inputs, output cache, madder) |
| Safe opening | `internal/readguard`, `cmd/readguard`; `pkg/toolbox` |
| Facade | `pkg/moxy` (Open, Tools, Call, Caller, Result); a `go-pkgs` flake output |
| Proxy around it | `internal/proxy`, `mcpclient`, moxyfile, narrowing, credentials, OAuth, async |

No consumer's build against `go-pkgs` is verified (U). Linking the library does
not give any tool: tools are TOML files plus scripts that nix wraps.

## 2. Step 0: what was fixed (moxy#532)

Fixed with tests. **This change deploys nothing**: a running moxy keeps the old
behaviour until it is updated.

- **T** `folio.glob`: the pattern can no longer leave `path` (absolute and `..`
  patterns are refused; matches that resolve outside are dropped). `path` is now
  opened through `readguard`, so a link out of the read policy is refused.
- **T** `folio.ls` and `folio.du`: `flags` accepts only listed display options.
- **Behaviour change:** an absolute glob pattern is now an error that says to
  pass the directory as `path`. Matches through a linked directory that leads
  outside are no longer listed.
- **T** `folio.tree`: `follow_symlinks` walked into whatever a link pointed at.
  `true` is now refused (fixed after the operator's later go).

**The fix does not make folio scopeable to one directory** (R): the floor is far
wider than a directory and is the fallback when no broker answers; `readguard`
never sees an in-process gate; under `delegate` with no hook the server does not
enforce "ask"; large `folio.read` and `rg.search` output has no cap. Rule for
spinclass's version: a scope is a hard limit checked first, with no floor and no
fallback; policy may only narrow it (principle 4).

## 3. The shape after the rulings

```
   session (tools run inside it, for now)      juggler run (inside the unit)
        |                                             |
        v                                             v  a SUBSET
   +--------------------- spinclass ------------------------+
   | owns folio: file views, backends, scope, tool schemas  |
   +-------------------------+------------------------------+
                             | imports only what it needs
   ==========================|=============================== moxy (library, later only)
                             v
              pkg/perms, pkg/moxy, results/blobs   <---- clown also imports
                             ^
   moxy binary, other moxins, krone origin: stay until each has a new host
```

## 4. What moxy must export for spinclass's folio

**For the first slice (read, list, glob, grep beneath one root): nothing.** It
is spinclass's own package. moxy hands over reference material:

| Material | Use |
|---|---|
| `moxins/folio/*.toml`, `moxins/rg/search.toml` | tool names, input schemas, descriptions |
| `moxins/folio/bin/` | behaviour (shell; not meant to be copied) |
| `zz-tests_bats/folio_*.bats`, `read_symlinks.bats`, blob tests | behaviour and security cases, including `folio_containment.bats` |
| `internal/readguard` | the open-without-following approach; it is not "beneath one root" |

For a safe slice I suggest `openat2` with `RESOLVE_BENEATH`; that is advice,
since the design is spinclass's.

**Later, only on request**, each as a small public package:

- **Results and blobs** (now `internal/native`): the cache threshold, the
  `madder://blobs` summary, and blob URIs as inputs. Needed if spinclass's tools
  should truncate and cache as moxy's do. Today agents read many spilled moxy
  results with `folio.read`, so **spinclass's `read` must accept blob URIs**
  before sessions lose moxy's (see also moxy#475). Under a scope, limit it to
  blobs the caller was given: a digest names any blob in the store.
- **`pkg/perms`** (public already): `Op`, `Resource`, `Decision`, if both sides
  should share the words. It imports `internal/native` and `pkg/toolbox`.
- **`pkg/policybroker`** is unchanged. spinclass's folio needs no broker for its
  own calls; moxy's remaining tools still ask it.

## 5. What standalone moxy and the krone origin lose

**When folio leaves (all 16 tools):** `read`, `glob`, `ls`, `tree`, `du`, `df`,
`file-type`, `tar`, `write`, `mkdir`, `cp`, `mv`, `rm`, `link`, `chmod`,
`mktemp`.

- Standalone moxy has no file tools, and no folio way to read its own spilled
  results (`jq`, `rg.search` and `toolbox` also take blob URIs; the server
  instructions text names folio and must change).
- **`folio-perms` must move first.** `rg`, `jq`, `toolbox`, `env`, `chix`,
  `arboretum`, `piers` and `readguard` use it as their floor (R). Move it to a
  shared place before deleting the moxin.
- `allow-tools` entries, hook and auto-allow lists, and profiles that name
  `folio.*` stop matching. Keeping the names on spinclass's side avoids most of
  this.
- **U:** whether the krone origin serves folio at all. circus owns that config.

**Migration:** (a) spinclass reaches parity, or each dropped tool is named;
(b) sessions switch, one host at a time (principle 30), with moxy's folio
disabled by `disable-moxins`, which flips back; (c) delete the moxin after no
consumer uses it.

**When moxy becomes library-only:** every command goes: `serve mcp`,
`serve-http`, the PreToolUse hook, the Claude plugin, `status`, `add`,
`narrows`, `check-moxins`, `serve-moxin` and the rest. The krone origin
(`moxy serve-http` without clown, per spinclass#438) then needs a new host,
clown or spinclass. Nothing should remove the binary until that host runs there.
Other repos that gate on `moxy check-moxins` or call `moxy narrows` need a
replacement too (R for juggler and `narrows`, from a circus plan; others U).

## 6. What removing moxyfiles affects

| moxyfile does today | Needs a replacement |
|---|---|
| child servers (`[[servers]]`, ephemeral, lazy, credentials, OAuth) | how clown or spinclass declare them |
| moxins: `builtin-native`, `disable-moxins`, search path | the same |
| tool allowlist: `allow-tools`, `pass-tools`, annotation filters | the tool subset a caller gets |
| narrowing (FDR 0013): `moxy narrows`, `MOXY_PARENT_MOXYFILE` | **a check that a subset is no wider than its parent's** |
| `permission-mode`, `enforce-allow` (FDR 0015) | the enforcing policy for hookless callers |
| hierarchy and `include` | where layered settings live |

- In the library, `pkg/moxy.Options` takes `Dir`, `File` or a
  `moxyfile.Config`. An in-memory config exists, but its type belongs to the
  TOML package. It must become plain Go options before the format can go (R).
- **juggler briefs carry an inline moxyfile today** (as the coordinator reports;
  a circus record shows a brief's `allow-tools` in use). Under ruling 4 a run
  gets a subset of spinclass's tools, so the brief needs a new way to name that
  subset, and a way to add its own tools (the review run's `emit_issue` and
  `done` are a circus moxin). Do not drop the moxyfile before both exist and a
  narrowing check replaces the old one.
- spinclass's guard on moxyfile edits becomes unneeded (U beyond its design
  record). moxyfile(5), FDR 0013 and FDR 0015 retire with it.

## 7. What I need from spinclass (not a design of its side)

1. Which tools it takes: the 16 folio tools only, or `rg.search` too? Which it
   drops.
2. Whether tool names stay `folio.*`, and how they appear to Claude and juggler.
3. When it needs the results-and-blobs package, and its madder pin.
4. How a caller's scope and tool subset are given, for sessions and juggler runs.
5. Whether and how juggler runs reach it from inside the unit.
6. Which parity point lets sessions switch, and which flag flips them back.
7. How the cutting-garden option affects the tool schemas moxy hands over.

## 8. Can a juggler run start a spinclass MCP server through moxy today?

An interim route while briefs still carry a moxyfile. From moxy's side,
probably; nothing was run.

- **R** A `[[servers]]` entry starts any stdio MCP server; the spawn code sets
  no environment, so the child inherits moxy's.
- **R** With a parent reference, a child may not add or change a server, and
  `command` is compared argument by argument. So a per-run root cannot be an
  argument unless the parent lists it. Pass it in the environment, set by the
  launcher; the server must refuse to start without it.
- **R** `allow-tools` must list the tools; under `enforce` they also need
  `enforce-allow`.
- **U** Whether a juggler brief accepts `[[servers]]`, and whether spinclass can
  serve without a session. A circus record (not rechecked) says a brief's
  `[env]` did not reach tools under `juggler run`.

## 9. Steps, in order

Sizes are rough guesses.

| # | Step | Where | Size |
|---|---|---|---|
| 0 | Fix moxy#532, `tree` included (done) | moxy | S |
| 1 | Slice 1: the file package, read/list/glob/grep beneath one root | spinclass | spinclass sizes it |
| 2 | juggler proof: a fake-model run that lists the tools offered | spinclass, juggler | S |
| 3 | Move `folio-perms` out of the folio moxin | moxy | S |
| 4 | Export results and blobs, if asked | moxy | M |
| 5 | spinclass parity; sessions switch behind a flag, host by host | spinclass | L |
| 6 | Delete moxy's folio moxin; fix instructions text, examples, tests | moxy | M |
| 7 | Replace moxyfile settings with Go options and a subset check | moxy, clown, spinclass, juggler | L, design first |
| 8 | Move the krone origin and other binary users; remove the binary | circus, moxy | L |

Review runs are not waiting: a circus plan records that the operator chose
circus's own limited tools for now (circus#514 tracks the switch). grit's move
is a separate track (moxy#482, spinclass#379). moxy#508 (one shared moxy per
host) assumes a long-lived moxy server and needs a fresh ruling under ruling 3.

## 10. Records that now conflict

To correct in the same change that acts on each ruling (principle 42). **Not
edited here.**

| Record | Conflict |
|---|---|
| spinclass#438 ruling 1: moxy owns the resource model and engine; no copy of moxy into spinclass | spinclass now owns folio. The engine stays in moxy "for now" |
| spinclass#438 ruling 9: copy moxy into clown, export the engine, keep standalone moxy | the move to clown is back to "examine later"; standalone moxy is no longer an end state |
| circus EPIC 0001 decision 17: clown runs the moxy library as the frontend proxy | still possible. Open: which frontend serves files now that spinclass owns the views; the epic's open "which frontend enforces what" note covers it |
| moxy FDR 0014: the binary's transports, including krone's, keep their shape | no longer the end state |
| moxy FDR 0013 and 0015, moxyfile(5), moxy `CLAUDE.md` (Folio, Moxyfile sections) | retire or change with steps 6 and 7 |

## 11. Risks

- **A gap in file access:** sessions lose moxy's folio before spinclass can
  write files or read blobs. Parity first, and keep the flag.
- **Shared floor:** deleting the moxin without step 3 breaks seven other users.
- **Two implementations** from step 1 to step 6.
- **Lost narrowing:** removing moxyfiles before the subset check lets a spawner
  hand a child more than it holds.
- **Inside the session:** a hole in a folio tool is bounded only by the
  session's own confinement. A scope check is not a sandbox.
- **madder version skew** between moxy's pin and spinclass's.

## 12. Open questions

| # | Question | Recommended default |
|---|---|---|
| 1 | Does "these tools" cover only the folio moxin, or also `rg.search` and the other file readers? | folio plus `rg.search` (a run needs grep); others stay for now |
| 2 | May sessions switch before all 16 tools have a replacement? | no, unless each missing one is dropped by name |
| 3 | Keep the names `folio.*`? | yes |
| 4 | Who hosts the krone origin once moxy is a library? | keep `moxy serve-http` until a named host runs it |
