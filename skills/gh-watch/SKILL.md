---
name: gh-watch
description: Connect this session to the gh-watch GitHub event watcher. Use when you want notifications for state transitions on GitHub Actions workflow runs (and, later, PRs and issues) without manually polling or writing a bash loop. The plugin monitor auto-starts on first invoke; register specific runs via get-hubbed.watch-run.
---

# gh-watch

Invoking this skill starts the gh-watch plugin monitor for this session. You
will receive a greeting notification of the form:

```
gh-watch connected: watching <N> target(s) at <dir> (interval=30s)
```

followed by one "resumed" line per already-registered target.

## Registering a run

Call the MCP tool `get-hubbed.watch-run` with:

- `repo` — `OWNER/REPO` (e.g. `amarbel-llc/moxy`)
- `run_id` — numeric workflow-run ID

State-transition notifications then arrive as:

```
run run:amarbel-llc/moxy#12345 queued/null → in_progress/null
run run:amarbel-llc/moxy#12345 in_progress/null → completed/success
run run:amarbel-llc/moxy#12345 done: success (https://github.com/…)
```

Completed runs are auto-dropped from the watch list.

## Managing targets

- `get-hubbed.watch-list` — table of currently-watched targets + live state.
- `get-hubbed.watch-remove <handle>` — drop a target (handle format is
  `run:<repo>#<run_id>`).

## One-shot companion tools

- `get-hubbed.ci-run-get <repo> <run_id>` — run metadata + per-job rows in
  one JSON object.
- `get-hubbed.ci-run-logs <repo> <run_id>` with optional `job_id` or
  `failed_only=true` — fetch logs directly without going through the
  watcher.

## Configuration

- `MOXY_GH_WATCH_DIR` — override the target-state directory (default
  `$XDG_STATE_HOME/moxy/gh-watch`).
- `GH_WATCH_INTERVAL` — seconds between polls (default 30).

## Scope

v1 only watches Actions workflow runs. PR, issue, and issue-comment watchers
are planned follow-ups; the monitor's target schema already reserves a
`kind` field for them.

## Requirements

Plugin monitors (the mechanism that delivers state-transition lines as
notifications) require **Claude Code v2.1.105 or newer**. On older
versions the MCP tools still work — `watch-run`, `watch-list`,
`watch-remove`, `ci-run-get`, `ci-run-logs` all function — but invoking
this skill will not start the monitor, so you won't see live
notifications. Use `get-hubbed.watch-list` to poll state manually if you
need to.
