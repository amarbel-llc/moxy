---
date: 2026-03-30
promotion-criteria: used in at least 2 moxyfiles across different repos
status: experimental
---

# Nix devshell wrapping for child servers

## What it does

The `nix-devshell` config key wraps a child server's command with
`nix develop <ref> --command`, so the server runs inside a Nix devshell without
the user needing to pre-enter it or manually wrap the command.

``` toml
[[servers]]
name = "man"
command = "maneater serve mcp"
nix-devshell = "."
```

Effective command: `nix develop . --command maneater serve mcp` (moxy splits the
command string on spaces)

## Interface

- **Config key:** `nix-devshell` (optional string, per-server)
- **Value:** any valid flake reference (`.`, `./path`, `github:owner/repo`,
  `github:owner/repo?dir=subdir`)
- **Behavior:** when set, `ServerConfig.EffectiveCommand()` prepends
  `nix develop <ref> --command` to the configured command and args

## Limitations

- Flake changes while a persistent server is running are not detected. The
  server continues running in the old devshell until `restart` is called.
- Each `nix develop` invocation evaluates the flake, which can be slow (\~1-5s).
  For ephemeral servers this cost is paid on every tool call.
- No global `nix-devshell` key --- must be set per-server.

## Next steps

- **Watch flake.lock for changes** --- auto-restart affected servers when the
  lock file changes, similar to how direnv re-enters on flake.lock changes.
- **Cached devshell** --- use `nix print-dev-env` or `nix develop --profile` to
  snapshot the environment once, then source it for subsequent spawns. Avoids
  re-evaluation on every ephemeral spawn.
