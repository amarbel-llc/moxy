toolbox(1)

# NAME

toolbox - pure applets piped over policy-blessed files, no shell

# SYNOPSIS

*toolbox.run* {"command": "_applet_ [_flag_...] [_operand_...] [| _applet_ ...]", "stdin": "..."}

# DESCRIPTION

*toolbox.run* is a moxy tool (the *toolbox* moxin) that runs a pipeline of
small programs that only compute: each reads bytes and writes text. None
writes a file, runs another program, uses the network or reads the
environment.

The command is parsed by moxy, never by a shell. It is a list of applet
invocations joined by *|*; the first reads _stdin_ (literal text, or a
*madder://blobs/*_digest_ URI whose bytes are fed in), each later one reads
the output of the one before, and the last one's output is the result.

# APPLETS

*b2sum*
	BLAKE2b checksums; see *toolbox-b2sum*(1).

*cut*
	select fields, characters or bytes; see *toolbox-cut*(1).

*digest*
	markl digest ids (sha256, blake2b256); see *toolbox-digest*(1).

*paste*
	merge lines of inputs; see *toolbox-paste*(1).

*sha256sum*
	SHA-256 checksums; see *toolbox-sha256sum*(1).

*sort*
	sort lines; see *toolbox-sort*(1).

*string*
	fish-style string operations (length, sub, upper, lower, trim, split,
	join, replace, match, pad, repeat, collapse); see *toolbox-string*(1).

*strings*
	printable character runs in binary inputs; see *toolbox-strings*(1).

*tr*
	translate, squeeze or delete characters; see *toolbox-tr*(1).

*uniq*
	collapse repeated adjacent lines; see *toolbox-uniq*(1).

*wc*
	count lines, words, characters and bytes; see *toolbox-wc*(1).

Each applet accepts only the flags its page lists. Flags that would write
(*sort -o*), run a program (*sort --compress-program*) or read a file the
permission gate never saw (*sort --files0-from*, *sha256sum -c*) do not
exist here.

# GRAMMAR

Words are separated by spaces or tabs. *'...'* quotes literally. *"..."*
quotes with *\\"* and *\\\\* as its only escapes. A quoted part and an
unquoted part next to each other form one word.

These characters are refused unquoted, because a shell would give them a
meaning toolbox does not have: *; & < > ( ) ` $ \* ? [ ] { } ~ # ! \\* and
newline. *$* and *`* are refused inside double quotes too. So there are no
redirects, variables, globs, command substitution, comments or *&&*/*;*
lists. Quote a tr class: *tr '[:upper:]' '[:lower:]'*.

Flags come before *--*; a word after *--*, or *-* alone, is an operand.
Short flags may be combined (*sort -rn*); a flag's value may be attached
(*-k2*, *--key=2*) or the next word (*-k 2*, *--key 2*).

# FILE OPERANDS

An operand of an applet that reads inputs is a path, a
*madder://blobs/*_digest_ URI (a cached tool result), or *-* for stdin.

Each path is a read resource. Before the call runs, moxy's permission gate
judges every one under the same policy as *folio.read*: the policy broker
(spinclass's [folio] stanza) when one answers, else *folio-perms read*.
Inside the policy a call runs with no prompt; outside it the call asks,
which an unattended (dontAsk) session cannot answer, so it is refused and
the refusal names the path. A command that does not parse is refused.

When the command runs, toolbox asks again about each path, then opens
exactly the path that was judged, refusing to follow any symlink on the
way (the leaf or a directory above it), and hands the applet only the open
descriptor. A path outside the policy is never opened, so it cannot reveal
even whether it exists.

A blob URI is fed through a pipe by moxy. It is judged as a blob (the
*madder:* anchor of the [folio] stanza), so chaining one call's cached result
into the next is as available as that grant.

# EXAMPLES

Byte size and SHA-256 of a release asset:

	wc -c dist/app.tar.gz
	sha256sum dist/app.tar.gz

The markl id of a cached result, which equals its blob digest:

	digest -a blake2b256 madder://blobs/blake2b256-...

Count distinct words, most frequent first:

	tr -s ' ' '\\n' | sort | uniq -c | sort -rn   (with stdin set)

# READING THESE PAGES

Through moxy, each page is also a read-only resource, *man://toolbox.1* and
*man://toolbox-<applet>.1*, so a session without the man tool can read them.

# SEE ALSO

*toolbox-b2sum*(1), *toolbox-cut*(1), *toolbox-digest*(1),
*toolbox-paste*(1), *toolbox-sha256sum*(1), *toolbox-sort*(1),
*toolbox-string*(1), *toolbox-strings*(1), *toolbox-tr*(1), *toolbox-uniq*(1), *toolbox-wc*(1), *moxin*(7),
*markl-id*(7)
