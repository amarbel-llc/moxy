# Standalone Moxin Installer

**Date:** 2026-04-13
**Issue:** [#109](https://github.com/amarbel-llc/moxy/issues/109)
**Status:** proposed

## Problem

Moxins like sisyphus (Jira), grit (git), get-hubbed (GitHub) are useful as
standalone MCP servers, but currently require the full moxy nix build to use.
Teammates who just want a single MCP server shouldn't need nix or the moxy
proxy.

## Solution

A tag-triggered release pipeline builds static binaries and per-moxin tarballs
via nix. A curl-installable script downloads the binary and a chosen moxin,
installs missing dependencies via brew, and registers the moxin with Claude
Code.

```
curl -fsSL https://github.com/amarbel-llc/moxy/releases/latest/download/install-moxin.bash | bash -s -- sisyphus
```

## Completed Prerequisites

- `moxy serve-moxin <name>` -- serves a single moxin as a standalone MCP server
  over stdio
- `moxy list-moxins` -- enumerates available moxins with name, tool count, and
  description

## Release Artifacts

Each tagged release (e.g. `v0.1.0`) publishes:

- `moxy-darwin-arm64.tar.gz` -- static Go binary
- `moxy-linux-amd64.tar.gz` -- static Go binary
- `<name>-moxin-darwin-arm64.tar.gz` -- per-moxin tarball
- `<name>-moxin-linux-amd64.tar.gz` -- per-moxin tarball
- `install-moxin.bash` -- the install script

### Building Through Nix

All artifacts are built through nix, reusing infrastructure from the
stark-mahogany branch:

**`moxy-static`** -- `buildGoApplication` with `CGO_ENABLED=0`, no
`wrapProgram`, no ldflags. Exe-relative moxin discovery (`SystemMoxinDir`)
resolves `../share/moxy/moxins/` relative to the binary.

**`mkBrewMoxin`** -- copies raw moxin directories without `wrapProgram`. Scripts
use `#!/usr/bin/env bash` and rely on ambient PATH. `@BIN@` is left
unsubstituted (resolved at Go runtime; see below).

**`mkBrewBunMoxin`** -- compiles TypeScript sources via `buildBunBinaries`,
extracts JS bundles into `lib/`, creates portable wrapper scripts:
```bash
#!/usr/bin/env bash
exec bun "$(dirname "$0")/../lib/<name>.js" "$@"
```

No nix store references in any output.

### Per-Moxin Nix Outputs

Each standalone-eligible moxin gets a flake output:

```nix
packages = {
  "standalone-env" = mkBrewMoxin "env";
  "standalone-grit" = mkBrewMoxin "grit";
  # ...
};
```

CI builds each with `nix build .#standalone-<name>`.

## `@BIN@` Runtime Resolution

Moxin TOML configs use `@BIN@` placeholders in command fields (e.g.
`command = "@BIN@/search"`). In nix builds, this is substituted at build time
with the nix store path. For standalone tarballs, `@BIN@` is left as-is.

`native.Server` gains a runtime fallback: before executing a tool command, if
`@BIN@` is present in the command string, replace it with
`filepath.Join(s.config.SourceDir, "bin")`.

```go
cmd := spec.Command
if strings.Contains(cmd, "@BIN@") {
    cmd = strings.ReplaceAll(cmd, "@BIN@", filepath.Join(s.config.SourceDir, "bin"))
}
```

This is backwards-compatible: nix-built moxins already have `@BIN@` substituted,
so the fallback never triggers for them.

## Install Script

**`install-moxin.bash`** -- hosted in the repo, attached to each release.

### Usage

```bash
# Interactive: presents a menu of available moxins
curl -fsSL .../install-moxin.bash | bash

# Direct: installs a specific moxin
curl -fsSL .../install-moxin.bash | bash -s -- sisyphus
```

### Flow

1. Detect platform (`uname -s` / `uname -m`) -> `darwin-arm64` or
   `linux-amd64`
2. Ensure `gum` is installed (brew on macOS, direct binary on Linux)
3. Fetch latest release tag from GitHub API
4. Download `moxy-<os>-<arch>.tar.gz` -> extract to `~/.local/bin/moxy`
5. If no moxin name argument: present menu via `gum choose` from hardcoded list
   of standalone-eligible moxins
6. Download `<name>-moxin-<os>-<arch>.tar.gz` -> extract to
   `~/.local/share/moxy/moxins/<name>/`
7. Check/install missing dependencies via brew (macOS) or apt (Linux)
8. Register: `claude mcp add <name> -- ~/.local/bin/moxy serve-moxin <name>`
   with `MOXIN_PATH=~/.local/share/moxy/moxins`
9. Print post-install notes (required env vars, if any)

### Idempotency

Re-running the script with a different moxin name installs only that moxin. The
moxy binary is overwritten if already present. Running with the same moxin name
is a no-op upgrade.

### Dependency Mapping

Hardcoded in the install script:

| Moxin                              | brew deps                       |
|------------------------------------|---------------------------------|
| env                                | _(coreutils only)_              |
| folio, folio-external              | jq, gawk                        |
| freud                              | python3                          |
| grit                               | git, jq                          |
| jq                                 | jq                               |
| rg                                 | ripgrep                          |
| get-hubbed, get-hubbed-external    | git, gh, jq, bun                 |
| hamster                            | go, bun                          |
| sisyphus                           | python3, pip:atlassian-python-api|
| just-us-agents                     | just, jq                         |
| man                                | pandoc, mandoc                   |

`pip:` prefixed deps are installed via `pip install <package>`.

### Excluded Moxins

- **chix** -- requires nix
- **jira** -- requires acli (commercial, not in brew)

### Graceful Degradation

Some tools within eligible moxins have optional dependencies:

- **man/semantic-search** needs `maneater` (custom binary). Without it, the tool
  returns an error but search, section, and toc work fine.
- **just-us-agents/run-recipe** uses `nix develop -c` when a `flake.nix` is
  present. Without nix, it falls back to plain `just`.

## CI Workflow

**New file: `.github/workflows/release.yml`**

Trigger: push tag matching `v*`.

### Jobs

**`build`** (matrix: `ubuntu-22.04`/`x86_64-linux`, `macos-15`/`aarch64-darwin`)

- Checkout tag
- Install nix via DeterminateSystems/nix-installer-action
- Build `moxy-static` -> tarball
- Build each `standalone-<name>` moxin -> per-moxin tarball
- Upload all tarballs as workflow artifacts

**`release`** (needs: build, runs-on: `ubuntu-latest`)

- Download all artifacts from build matrix
- Create GitHub Release for the tag
- Attach all tarballs + `install-moxin.bash`

### Coexistence

The existing `nix.yml` workflow (build + FlakeHub publish on master push) is
unchanged. The release workflow only fires on tags.

## Install Layout

```
~/.local/
  bin/
    moxy                              # static Go binary
  share/
    moxy/
      moxins/
        grit/
          _moxin.toml
          search.toml
          ...
          bin/
            search
            ...
        sisyphus/
          _moxin.toml
          get-issue.toml
          ...
          bin/
            get-issue
            ...
          lib/                         # bun moxins only
            get-issue.js
            ...
```

`MOXIN_PATH=~/.local/share/moxy/moxins` is passed as an environment variable
in the `claude mcp add` registration.

## Rollback

This is additive infrastructure -- no existing system is replaced.

- **Delete the GitHub Release** to remove all download artifacts and kill the
  install path
- **The install script** is fetched from the release, so deleting the release
  is sufficient
- **No changes to the nix build path** -- `moxy-moxins`, `mkMoxin`,
  `mkBunMoxin` are untouched
- **`@BIN@` runtime resolution** is a fallback that only activates when the
  placeholder is still present -- zero impact on nix-built moxins
