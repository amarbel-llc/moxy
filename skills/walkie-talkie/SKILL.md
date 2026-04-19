---
name: walkie-talkie
description: Connect this session to the walkie-talkie cross-session message bus. Use when coordinating with a peer Claude Code session in real time — the monitor auto-starts on first invoke and pushes inbound messages as chat notifications; reply via the walkie-talkie.send MCP tool.
---

# Walkie-Talkie

Invoking this skill starts the walkie-talkie plugin monitor for this session.
You will receive a greeting notification of the form:

```
walkie-talkie connected: session=<your-session-id> log=<path>
```

After that, any message addressed to your session id or to `all` arrives as a
notification with the raw log line.

## Sending

Call the `walkie-talkie.send` MCP tool with:

- `target` — peer session id (e.g. `chrest/eager-poplar`) or the literal
  `all` for broadcast.
- `body` — short message. **Must not contain newlines or tabs.** For long
  content, reference a file path, gist URL, or GitHub issue number in the
  body.

Sender id is set automatically from `$SPINCLASS_SESSION_ID`.

## Catching up

`walkie-talkie.backscroll` returns the last N lines of the bus log (default
20), unfiltered — including traffic between other sessions.

## Conventions

- Keep messages short and specific. The bus is for fast coordination, not for
  durable decisions (use GitHub issues and commits for those).
- One message per concrete ask or update. Don't batch unrelated items into
  one line.
- `SPINCLASS_SESSION_ID` is required. Sessions not launched via spinclass will
  see an error instead of silent misbehavior.

## Requirements

Plugin monitors (the mechanism that delivers inbound messages as
notifications) require **Claude Code v2.1.105 or newer**. On older
versions the MCP tools still work, but invoking this skill will not
start the monitor, and messages addressed to you will only appear via
`walkie-talkie.backscroll`.
