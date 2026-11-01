---
status: proposed
date: 2026-11-01
---

# Policy-broker protocol

## Abstract

This document specifies how a moxy asks a standing policy broker whether a
tool call may touch a resource. The wire is newline-delimited JSON over a unix
socket. Both sides send a `hello`; afterwards moxy sends one `check
{subject, op, resource}` per resource a call names and receives exactly one
verdict: `allow`, `ask` or `deny`. Moxy's compiled-in static floor applies in
exactly one situation: there was no answer. Permissions always fail closed.

## Introduction

Path policy for moxy's tools was a fixed rule in `moxins/folio/bin/folio-perms`
(inside the CWD, `/nix/store`, a few Claude directories and sibling repos),
reached through `perms-request = "dynamic"` exit codes. spinclass#438 rules
that the session manager owns the policy instead, and that moxy owns the
resource model and the engine (moxy#491; design record
`docs/plans/2026-11-01-438-policy-broker-design.md` in spinclass, sections 1,
3 and 4). Policy therefore arrives over a standing connection, not a process
per call. For a spinclass session the broker is `spinclass serve`; the
protocol does not depend on that, and any process that speaks it can broker.

The handshake and initiation are modelled on clown's RFC-0002 (the launcher
names the address, the service listens, the client dials) and the single-peer
broker shape on RFC-0005 (egress broker). Unlike RFC-0005 the broker is
long-lived and shared by the lifetime of a session, and unlike RFC-0002 the
address arrives in an environment variable rather than on a handshake line.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD",
"SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be
interpreted as described in RFC 2119.

## Specification

### 1. Addressing

The launcher (clown, clown#333) names the broker and exports it to the agent's
environment as `CLOWN_POLICY_BROKER` (agreed with clown, 2026-11-01). It is
the only variable the client reads for this protocol; the launcher sets it only
when the session has a broker. Two forms:

- a value starting with `/` is the absolute path of a unix socket (unconfined
  sessions: a 0600 socket in a 0700 per-session directory the broker creates);
- anything else is `host:port` over TCP (confined sessions: the loopback
  address of the in-unit relay, which forwards to the broker).

Both carry the same framing and messages. Unset or empty means no broker is
configured: moxy MUST behave exactly as it does without this protocol (the
floor decides, section 6). A value that is neither an absolute path nor a
`host:port` is a configuration error: no answer, with a diagnostic on stderr.

The launcher exports no session key to the agent's environment. The subject is
taken from the broker's hello (section 3). One optional expectation exists:
spinclass exports `SPINCLASS_SESSION_ID` into every session it launches; when
it is set, the client lists it as its one subject and so refuses a broker whose
session key differs, and when unset it adopts the broker's key. (That the two
strings are the same key is spinclass's to confirm; if they ever differ the
client fails closed to the floor.)

### 2. Framing

Each message is one JSON object on one line, terminated by `\n`, UTF-8, no
embedded newlines. A message is at most 64 KiB; an oversize line is a protocol
error. Every message has:

| Field | Type | Meaning |
|---|---|---|
| `v` | integer | protocol version, `1` in this document |
| `type` | string | `hello`, `check`, `verdict`, or `error` |

Unknown fields MUST be ignored. An unknown `type` from the broker is treated as
no answer for the pending check; from moxy the broker SHOULD send `error` and
continue.

### 3. Hello

Immediately after connecting, each side sends a `hello` without waiting for the
other.

Client:

```json
{"v":1,"type":"hello","client":"moxy 0.6.46","capabilities":["check"],
 "subjects":[]}
```

`subjects` lists the subjects the client asks to serve. An empty or absent
list means "the broker's own session": the client adopts the `session_key` of
the broker's hello as its subject. A client that does know its subject (an
embedder, or a later principal identity) lists it, and then MUST refuse a
broker whose `session_key` is not among them.

Broker:

```json
{"v":1,"type":"hello","broker":"spinclass serve","capabilities":["check"],
 "session_key":"<session key>"}
```

- `v` is the highest version the sender speaks. The connection runs at the
  lower of the two; with a single version today, a differing `v` means the
  hello is refused.
- `capabilities` is a list of strings; unknown entries are ignored. `check` is
  the only capability defined here and is REQUIRED of the broker.
- `session_key` is the broker's session key and, for now, the subject. When
  the client's `subjects` is non-empty it MUST refuse a broker whose key is not
  among them (a wrong broker answers for the wrong session); when it is empty
  the client adopts the key. A missing or empty `session_key` is a refused
  hello. A refused hello closes the connection and is no answer.
- A broker MUST authenticate the peer by its own means (for spinclass,
  `pkgs/peercheck`) before answering; the connection authorizes "this peer may
  ask for these subjects" and nothing else. A broker that does not authorize a
  subject in the client's hello MUST send `error` with `code: "subject"` and
  close.
- The client MUST wait for the broker's hello before sending any `check`, and
  treats a hello not received within the hello deadline (default 1 s) as a
  refused hello.

### 4. Check and verdict

```json
{"v":1,"type":"check","id":7,"subject":"<session key>","op":"read",
 "resource":{"kind":"file","uri":"file:///abs/path"},"deadline_ms":100}
```

| Field | Meaning |
|---|---|
| `id` | client-chosen integer, unique on the connection among pending checks |
| `subject` | the subject the call is made for; MUST be one named in the client's hello |
| `op` | `read`, `write` or `exec` |
| `resource` | `{kind, uri}`; `kind` is `file` (`file:///` absolute path) or `blob` (`madder://blobs/<digest>`) |
| `deadline_ms` | how long the client will wait; the broker MAY drop a check it cannot answer in time |

The broker answers each check with exactly one:

```json
{"v":1,"type":"verdict","id":7,"verdict":"allow","reason":"stanza 3"}
```

`verdict` is one of:

- `allow`: the call may run.
- `ask`: a human must decide. Moxy maps it to its existing prompt path (the
  same outcome as a `perms-request` exit 1); under a client that cannot prompt
  (Claude Code `dontAsk`) the existing ask-to-deny conversion applies.
- `deny`: the call is refused.

`reason` is OPTIONAL, for display and audit. Any other `verdict` value, a
verdict whose `id` is not pending, or a missing `id` is a protocol error. The
broker always answers one of the three; its configuration carries a rule for
the unmatched case. Checks MAY be pipelined; verdicts MAY arrive in any order.

A call naming several resources is checked per resource and the strictest
verdict wins (`deny` over `ask` over `allow`); moxy MAY stop at the first
`deny`. A resource that is `/dev/fd/*` (the pipe a `madder://blobs/` argument
is substituted with) or empty names nothing and is not checked.

Which tools are checked is declared by the tool, not guessed: a moxin tool
names its resource inputs and the operation on each in `[resources.<input>]`
(moxin(7) RESOURCES). Only tools on the `dynamic` permission tier are
consulted; a verdict never lifts `each-use` or `always-allow`.

### 5. Errors

```json
{"v":1,"type":"error","id":7,"code":"subject","message":"..."}
```

`id` is present when the error answers a specific check. An `error` for a check
is no answer for that check. Codes: `subject`, `protocol`, `internal`.

### 6. The floor: no answer only

Moxy has a compiled-in static floor: today's `folio-perms` rule, unchanged. It
is the whole of moxy's behaviour when no broker is configured. When a broker
is configured, the floor applies for a check in exactly these cases, which
together are **no answer**:

1. the connection is not established (unreachable, refused, no hello yet);
2. the hello was refused (version, wrong session key, missing capability);
3. the deadline passed before a verdict arrived;
4. the connection dropped or the verdict was an `error` or a protocol error.

In every other case the broker's verdict is final, including when it is
stricter or looser than the floor. When a call names several resources and the
broker answers some but not others, the floor decides the whole call and the
stricter of the floor and the strictest answered verdict stands; a broker
`deny` is always final.

Where the call is judged follows moxy's permission mode. Under `enforce`
(FDR 0015) the funnel resolves every call, so `allow` runs it, `ask` and `deny`
refuse it (nobody can answer an `ask`). Under `delegate` a client-side hook may
sit in front of moxy and may already have prompted a human for an `ask`, so the
funnel acts on `deny` alone and leaves `ask` to the client; no answer changes
nothing (moxy#485). A verdict never widens the floor's answer by
being absent, and no absence, error, timeout or malformed message ever
resolves to `allow` by itself: the floor is the only fallback, and the floor
may itself say `ask` or `deny`. In particular a crashed or hung check MUST NOT
resolve to `ask` by accident (moxy#265).

### 7. Reconnect

Moxy dials at startup and MUST NOT block startup or tool listing on it: until
the first hello completes, every check is no answer. On connection loss or a
refused hello moxy reconnects with exponential backoff, starting at 100 ms,
doubling to a cap of 5 s, with jitter. A check in flight when the connection
drops is no answer. A new connection repeats the hello; checks are never
replayed across connections.

### 8. Deadline

The per-check deadline default is a compile-time constant of the client.
**Unmeasured:** the design record says tens of milliseconds, to be measured
with a unix-socket round-trip benchmark and re-measured with the real
evaluator. Until then the default is 100 ms; the value is carried in
`deadline_ms` so the broker sees the budget.

### 9. The PreToolUse hook is a second client

Under Claude Code the prompt is decided in moxy's PreToolUse hook, a fresh
short-lived process per tool event, before the call reaches the server; a
verdict held only inside the server could not remove a prompt. The hook is
therefore a second broker client on the same wire: it reads
`CLOWN_POLICY_BROKER`, dials, completes the hello, sends its checks and exits.
It dials lazily, only when a call names a declared resource, and waits at most
250 ms for the hello; past that the check is no answer and the floor decides,
like every other client. It caches nothing between events. The server still
checks each call over its standing connection (section 6, where the funnel's
treatment under `delegate` is described), and both fail closed on no answer.
The per-call dial cost is **unmeasured**; if it proves to matter it is reported
with numbers rather than hidden behind cached verdicts.

## Security Considerations

The address is a capability only in combination with the broker's peer
authentication; file permissions on the socket and its directory (or, over the
confined relay, the relay's cgroup) are the first line, not the only one. The `subject` in a check is asserted by the client and
authorized per connection by the hello, so a client cannot name a subject its
connection was not authorized for. A broker that is down or slow degrades
moxy to the floor, which is today's rule; it never degrades to something looser
than that rule. The floor is part of the trust base and MUST NOT be configurable
through the broker connection.

## Compatibility

A moxy with `CLOWN_POLICY_BROKER` unset is byte-for-byte today's behaviour,
including standalone `moxy serve-http` as run by krone's `mcp-origin`. A broker
that does not understand a newer `v` refuses the hello, which is no answer, so
a newer moxy still has the floor.

## References

### Normative

- RFC 2119.
- clown RFC-0002 (plugin protocol handshake), RFC-0005 (egress broker).
- spinclass `docs/plans/2026-11-01-438-policy-broker-design.md`, sections 1, 3, 4.

### Informative

- moxy#491, moxy#490, moxy#485, moxy#265; clown#333; spinclass#438.
