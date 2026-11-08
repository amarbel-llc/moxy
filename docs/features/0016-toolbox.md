---
status: experimental
date: 2026-11-02
issue: moxy#500 (moxy#499 folded in)
related: moxy#490 (shared read-path policy, the [folio] stanza), RFC 0002
  (policy broker), moxin(7) RESOURCES and RESOURCE-AS-FD COMPOSITION,
  markl-id(7), toolbox(1)
promotion-criteria: >
  experimental → testing when an unattended (dontAsk) session checks a
  downloaded release asset's byte size and sha256 and filters a cached tool
  result through a pipeline, with no permission prompt and no shell, and the
  applet set has gone a release without an applet gaining a flag that
  writes, executes or reads an unjudged file.
---

# toolbox: pure applets over policy-blessed resources

## Problem Statement

An unattended session has no shell, so it cannot do the small things a shell
pipeline does: check a release asset's byte size or digest (moxy#499; a worker
reported exactly this on 2026-11-02), count lines, sort, cut a column. Giving
it a shell would give it everything else a shell does (write, exec, network).

What it needs is a busybox-like set of programs that only *compute*, reading
resources the session is already allowed to read. Which resources may be read
is not the applets' business: it is decided by the same policy that decides
for `folio.read` (moxy#490: spinclass's `[folio]` stanza through the policy
broker, folio-perms as the floor).

## Interface

One moxin, `toolbox`, with one tool, modelled on the Bash tool:

```
toolbox.run {command: "wc -c dist/app.tar.gz", stdin?: "..."}
toolbox.run {command: "sort -n nums.txt | uniq -c"}
toolbox.run {command: "digest -a blake2b256 madder://blobs/blake2b256-…"}
```

`command` is a pipeline of applets joined by `|`; the first reads `stdin`
(literal text, or a `madder://blobs/<digest>` URI whose bytes are fed in),
each later one reads the previous one's output, the last one's output is the
result. The tool description stays short and defers usage to toolbox(1) and
one toolbox-<applet>(1) page per applet, built from `moxins/toolbox/man/`
by the moxin's derivation and installed on the man path through `moxy-man`.

Round-one applets: `wc`, `sha256sum`, `b2sum`, `digest`, `tr`, `sort`,
`uniq`, `cut`, `paste`. All but `digest` run the pinned coreutils program;
`digest` runs in-process and prints markl ids (`sha256`, `blake2b256`), so a
blob's `blake2b256` id equals the digest in its URI. Byte size is `wc -c`.

`string` (moxy#502, which folded in #498) is fish's `string` builtin as one
in-process applet: `string <subcommand> ...`, an applet whose first word
names a subcommand parsed with its own flag table. The policy grammar uses the
same parse. Its operands are strings, not files (as `tr`'s sets are), so it
reads nothing the gate would judge. It inherits `toolbox.run`'s tier; it is
not always-allow. RE2 only, 16 MiB in and out per stage. `escape` and
`unescape` (moxy#505) quote for a context: `--style` shell (the default), url
or json. JSON output is not done yet. See toolbox-string(1).

`strings` (moxy#184) is in-process too: the printable runs of an input, one
per line, so `strings bin | string match -re PATTERN` checks a build for a
string with no shell. See toolbox-strings(1).

## The grammar

`pkg/toolbox` parses the command; nothing hands it to a shell.

- Words split on spaces and tabs. `'…'` quotes literally; `"…"` quotes with
  `\"` and `\\` as its only escapes. Adjacent parts join into one word.
- Unquoted, the characters a shell gives meaning to are refused, not taken
  literally: `; & < > ( ) ` $ * ? [ ] { } ~ # ! \` and newline (`$` and `` ` ``
  inside double quotes too). So there are no redirects, variables, globs,
  substitution, comments, `&&` or `;`, and a command never means something
  different here than it would in sh: it either means the same or is refused.
- The first word of a stage names an applet. Every later word is checked
  against that applet's flag table: short flags (combinable, value attached or
  next), long flags (`--key=V` or `--key V`), `--` to end flags, `-` for stdin.
  An unknown applet or flag, a bad value or a wrong operand count is an error.

## What makes an applet admissible

1. **Pure.** Output is a function of input bytes and flags. Programs run with
   an environment of `LC_ALL=C.UTF-8` alone.
2. **No side effects.** Writes nothing but stdout and stderr; no network.
3. **No exec.** Runs no program but itself.
4. **Typed flags only.** Each applet's flags are a table in `pkg/toolbox`;
   there is no passthrough. Flags that would write (`sort -o`, `uniq`'s output
   operand), run a program (`sort --compress-program`) or read a file the gate
   never saw (`sort --files0-from`/`--random-source`/`-T`, `sha256sum -c`,
   `wc --files0-from`) are simply absent, so rule 3 and the read policy hold
   by construction (the jq lesson, moxy#495, applied up front). `sort -R` is
   absent as impure.

Candidates for later rounds, under the same rules: `head`/`tail`, `comm`,
`cmp`, `base64`, `nl`, `fold`, `rev`, `tac`. Excluded: `sed`/`awk` (can write
files or run commands), anything reading the clock, network or environment.

## How an argument becomes a checked resource

This builds on moxy's two existing mechanisms; the parser plugs into each per
argument, because which words are files is only known after parsing.

**`[resources.command]`** (moxin(7) RESOURCES) gains one key:

```toml
[resources.command]
op = "read"
grammar = "toolbox"
```

`pkg/perms.ResourcesOf` parses the command with `pkg/toolbox` and yields one
read resource per *file operand* (a path, made absolute and canonicalized like
every other resource, or a blob URI). Applet names, flags, flag values and
`tr`'s sets are not resources, and nothing is matched by name inside an
applet. The broker (spinclass's `[folio]` stanza) therefore judges each file
operand exactly as it judges `folio.read`'s `file_path`.

**The floor** is `[dynamic-perms]` running `toolbox perms`, which parses the
command and runs `folio-perms read` on exactly its file operands. A command
that does not parse is denied there (exit 2), and since it yields no
resources the broker never answers for it, so an unparseable command can
never come out allowed, with or without a broker.

**`substitute-args = ["command", "stdin"]`** (RESOURCE-AS-FD COMPOSITION):
moxy opens each blob URI in the command on a pipe and rewrites it to
`/dev/fd/N`, as for jq's `files`. The gate saw the URI, before substitution,
as `{kind: blob}`; spinclass judges that by the `madder:` anchor, so chaining
cached results is as available as that grant. A blob URI given as `stdin` is
not a resource and is not asked: it is the caller's earlier result fed back.

So the tier is *allowed for policy-blessed inputs only*: a call whose
operands are all blessed, or that reads only stdin, runs with no prompt (so
under dontAsk it just runs); an operand outside the policy asks, which under
dontAsk is refused, and the refusal names that operand. The applets are not
on spinclass's PreToolUse auto-allow list (`hooks/moxy_read.go`) and should
not be: the broker verdict through moxy is the single engine.

Nothing in spinclass changes, and the path engine (folio-perms,
`pkg/policybroker`) is unchanged; `pkg/perms` gains the `grammar` branch in
`ResourcesOf` and exports `CanonicalPath` (agreed with moxy/sunny-sumac, the
#490 lane).

## Opening: the judged path, no symlinks

The gate judges a path with its parent directories resolved and its leaf as
named (spinclass's folioeval and folio-perms alike), so opening the operand
afterwards would follow a symlink leaf to wherever it points. `toolbox run`
therefore opens files itself, after asking again, and the applet only ever
receives descriptors:

1. For each file operand, compute `perms.CanonicalPath` (the path the gate
   judges) and re-ask: the broker when `CLOWN_POLICY_BROKER` answers, else
   `folio-perms read`. Only a deny refuses here: an ask was the hook's to put,
   to the user or refused under dontAsk, before the call reached toolbox, and
   toolbox cannot ask. A broker that is down or hangs is no answer and falls
   to the floor, never to allow.
2. Open exactly that path with `openat2(RESOLVE_NO_SYMLINKS)`: any symlink,
   the leaf or a directory above it swapped in after the verdict, fails the
   open. The file opened is the path judged. Non-regular files are refused.
3. Pass the open files to the applet as inherited descriptors
   (`/dev/fd/3`…); an operand of `/dev/fd/N` is accepted only for N ≥ 3
   (the pipes moxy substitutes for blob URIs).

Because the check comes before the open, a path outside the policy is never
opened and cannot reveal whether it exists.

Residual: without a broker, the floor never denies a read (it allows or
asks), so step 1 adds nothing there and the protection is step 2. A parent
directory renamed between the hook's verdict and the open to point the same
canonical name at another directory is judged by name, as everywhere else in
the policy. On non-Linux builds `O_NOFOLLOW` covers only the leaf.
`folio.read` has the symlink-leaf case today; that is #490's lane.

## Confined sessions

A confined unit serves clown's in-unit moxyfile, which today disables most
moxins, so toolbox is off there until clown's profile library enables it
(clown's lane and the operator's call; requested after this lands). In a
confined unit `~` paths mean the unit's agent home, for toolbox operands as
for folio.

## Out of scope

- Shell syntax, variables, globbing, redirection, command substitution.
- Writing anywhere, including a "save result to path" option: results reach
  the blob store through moxy's result caching, never a caller-named path.
- Digests outside markl's set (`sha1`, `md5`, `sha512`).
- Verifying a digest against an expected value: the caller compares.
- Naming blob inputs in output: a blob URI reaches the applet as
  `/dev/fd/N`, so that is the name it prints.
- Exposing the manpages as resources (to supplant the man tool): a separate
  issue.
