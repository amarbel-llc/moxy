# One file-access path: whatever opens a file also judges it (moxy#536)

Date: 2026-11-27. Status: **proposal, no code**. Labels: **R** read in code,
**U** not verified.

> **DRAFT: this failed its principles review. Build nothing from it until the
> findings are resolved on moxy#536.** The review (8 conflicts, 8 unclear)
> found, among other things, that the load-time check would miss tools which
> declare no resources, that "safe now" for an attended "ask" is unsupported,
> that a scope does not bind programs given only a name, and that the new
> refusals have no switch back. The operator accepted merging it as a draft.

## The problem, as one story

An agent calls a tool with a path. A check reads that text and says "inside the
project, allowed". Then moxy swaps a blob reference for `/dev/fd/3`, a shell
script runs `dirname` on it, and `cp` follows a link. What gets opened is not
what was judged.

Every hole found in moxy#532 to #535 has this shape (R). Four review rounds each
found more. Patching one difference at a time does not converge.

```
 TODAY                                      PROPOSED

 args (text)                                args
   |--> check A: judges the text              |--> parsed ONCE into typed values
   |      (Go resolver or bash floor)         |      path | blob | plain text
   |                                          v
   |--> substitution rewrites text         +----------------------------+
   |--> readguard judges again (reads)     | the guard (one Go package) |
   v                                       |  resolve -> judge -> OPEN  |
 shell script: dirname, split, cp, rm      +-------------+--------------+
   |                                                     | handles, not names
   v                                                     v
 whatever that opens                        a Go file verb, or a program
                                            given an open descriptor
```

## 1. One rule

**The thing that was judged is the thing that is used.** The guard returns a
handle, and the tool cannot turn it back into a different file.

| Operation | Handle the guard gives out |
|---|---|
| read, list, stat | an open descriptor of the object |
| create, replace, remove, rename, link | the open **parent directory** plus one leaf name with no `/` |
| chmod | an open descriptor; a link leaf is judged by its target |
| search a tree | an `os.Root` of the directory |

A leaf that is a link is followed only if its target is allowed, for writes as
for reads. `os.Root` in Go 1.26, the version moxy builds with, has open, create,
mkdir, remove, rename, link, symlink and chmod (R). It confines to a root; it
does not by itself refuse links inside the root, so the guard still checks the
leaf. Whether its calls are enough for every verb without `openat2` is **U**;
slice 3 proves it with swap tests.

## 2. The five parts

**a. Arguments are parsed once.** `[resources.<input>]` becomes the only way a
file reaches a tool, with a type: `path`, `paths` (a real JSON array),
`path-or-blob`. A blob is a typed value that the guard turns into a descriptor.
No more rewriting text inside an argument, and no space-splitting (`rm`).
Tools with their own grammar (`toolbox`) parse first and hand the guard their
operands.

**b. One implementation of the rules.** The allowed directories and path
resolution move into Go. `folio-perms` keeps its name and exit codes, as a thin
call into that code, so the moxins that use it do not change (about 30 files
name it, R). The hook, the server, the broker client and the guard then compute
the same resources by calling the same function. The broker is never sent a path
the guard could not fully resolve: that is a refusal.

**c. Writes get the guard too.** `write`, `mkdir`, `cp`, `mv`, `rm`, `link`,
`chmod` become Go verbs in one binary, acting on handles. No shell sits between
the check and the change. `mv` asks for write on its source.

**d. What "ask" means at the moment of opening.** Today the guard refuses only
"deny", so an "ask" passes, trusting a hook that may not exist. Proposed:

| Caller | At open, the guard requires |
|---|---|
| attended, with a hook that prompts | not denied. Safe now, because hook and guard compute the same resources from the same code |
| `enforce`, `dontAsk`, no hook, or any scoped caller | **allow** |

The server tells the guard which row applies. If it says nothing, the strict row
applies (principle 6). A path that changes between the prompt and the open still
differs; closing that needs the approval to name what was approved, which a
hook cannot pass to the server today (U). It stays a stated limit.

**e. Programs that need names.** Not every tool can take a handle. Each tool is
put in exactly one class, declared in its TOML:

| Class | Examples | Guarantee |
|---|---|---|
| handle | folio verbs, `toolbox`, `jq` files | full |
| directory entered by handle | `rg`, `tree`, `du`, `ls` | the start is held; the program must not follow links |
| name only | `nix`, `git`, `ast-grep` by extension | judged, not held; always shown as such |

A load-time check refuses a tool that declares a path resource and no class, as
moxy#520 already does for undeclared path readers. That is the mechanism that
keeps the next tool from opening its own side door (principle 1).

## 3. A scope beneath one directory

A caller may carry a root. The guard checks it first and opens beneath it. It is
a hard limit: no floor, no fallback, and policy may only narrow it. Blobs under
a scope are limited to ones the caller was given. This is what a juggler run
needs. It limits what these tools open; it is not a sandbox.

## 4. How this relates to the held ownership question

The operator is working out how moxy's tools could act on **resources that
spinclass provides**. This design does not answer that and should not. It does
create the seam: tools ask for handles through one interface (resolve, judge,
open). Today the local filesystem implements it. Another provider could
implement it later without the tools changing. Whether that interface is a
process, a socket or a Go package is left open on purpose.

## 5. Slices, in order

Each is tests-first and ships alone. Sizes are rough guesses in changed lines.

| # | Slice | Closes | Size |
|---|---|---|---|
| 0 | Never send the broker an unresolved path; fix the two `glob` errors | principles #6, #49 on moxy#535 | S, <100 |
| 1 | One Go policy; `folio-perms` calls it. Today's bats cases become the parity suite | moxy#535 item 2; principles #9, #45 | M, 400 to 700 |
| 2 | The guard requires allow unless told the caller is attended | the "ask passes" gap | M |
| 3 | Go write verbs on handles; delete the write scripts | moxy#535 items 3 and most of 5 | L, 800 to 1200 |
| 4 | Typed arguments and blob values; remove text substitution for paths | #535 items 5, 6; the blob-suffix family | M |
| 5 | Classes for every path tool, with the load-time check | side doors | M |
| 6 | Scoped root | the juggler need | M |

Slice 1 comes first because everything else relies on there being one resolver
(principle 27). Slice 3 removes the most dangerous remaining gaps: if writes
worry you most, it can move ahead of 2.

**Proof that it worked:** one conformance test that, for every tool with a path
resource, swaps a link in after the judgement and asserts the tool still acts on
the judged object or fails. It runs in the gate, so a new tool is covered
without anyone remembering to.

## 6. Risks

- **Behaviour changes.** Writes through links and some `ask` cases will start to
  be refused. Roll out behind the usual per-host deploy, canary first.
- **Rewriting the write tools** can lose small behaviours of `cp`, `mv` and
  `rm`. Today's bats lane is the floor; add cases before porting each verb.
- **Two paths during the move.** Delete each script in the slice that replaces
  it, never later.
- **Speed.** A Go floor replaces a bash one that forks `git` and `realpath`; I
  expect no slowdown but have not measured it (U).
- **Linux only** for the strongest guarantees; elsewhere fail closed.

## 7. Questions for the operator

| # | Question | Recommended default |
|---|---|---|
| 1 | Should writes follow a link leaf whose target is allowed? | yes, as reads do; otherwise refuse |
| 2 | Is "ask passes when attended" acceptable once there is one resolver? | yes, with the strict row as the default |
| 3 | Order: slice 1, 2, 3, or writes (3) before the "ask" change (2)? | 1, 3, 2: writes are the worse exposure |
| 4 | May `rm`'s `paths` string become an array (a tool interface change)? | yes |
| 5 | Keep `nix` and `git` in the name-only class, or restrict them further? | keep, clearly labelled |
| 6 | Wait for the resource-provider decision before slice 4? | no for 0 to 3; revisit before 4 |
