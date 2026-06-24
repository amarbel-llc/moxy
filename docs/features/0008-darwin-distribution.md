---
status: exploring
date: 2026-06-24
promotion-criteria: A distribution path is selected and a working install flow exists for at least the binary-only case on a Nix-less Mac.
---

# Distributing moxy to a Nix-less Darwin user

## Problem Statement

moxy is built and run via Nix today: the `moxy` binary bakes `/nix/store`
paths (`defaultSystemMoxinDir`, `defaultMadderBin`) at build time, and each
moxin resolves its external tools (bash, jq, git, ripgrep, gh, pandoc, …)
through a nix-wrapped PATH. A teammate on a Mac who does **not** have Nix
cannot install or run moxy. We want a low-friction way to hand moxy (or a
single moxin) to such a user — ideally close to "one command, then run it" —
without requiring them to adopt Nix.

This record captures the distribution options explored, what was proven, and
the hard constraints that rule some options out, so a future contributor
doesn't re-tread the dead ends.

## Interface

Three distribution vehicles were explored. None is yet adopted as *the* path;
this section documents each one's shape and verdict.

### Option A — Apple `container` OCI image (proven this session)

Nix cross-compiles the moxy binary to `aarch64-linux` (pure Go, CGO off — no
Linux build VM), assembles a Linux OCI image on Darwin via
`dockerTools.buildLayeredImage`, and skopeo-transcodes it to OCI-archive
layout. Apple's `container` loads and runs it in a per-container micro-VM.

- Flake outputs: `moxy-linux` (cross binary), `moxy-docker-image`
  (buildLayeredImage), `moxy-oci-image` (skopeo → OCI archive).
- Justfile recipe: `container-prototype` runs the full
  build → `container image load` → `container run` loop.
- Consumer needs: Apple's `container` installed (NOT preinstalled on macOS;
  available via `brew install container` from `homebrew/core`, or a signed
  `.pkg`), plus `container system start` (one-time, downloads a Linux kernel).

**Verdict:** works end to end (verified: the image booted a `linux/arm64`
micro-VM and printed `moxy <version>`). Meets "self-contained artifact" but
not "single file, zero installs" — the consumer must install an OCI runtime
first. Two substantive caveats: (1) the prototype image is the **bare moxy
binary** — no moxins — and bundling them needs a Linux Nix builder (see
Limitations / moxy#386); (2) moxins running **inside** the VM operate on the
VM's filesystem, not the user's Mac files, unless the cwd is bind-mounted in —
a real concern for file-touching moxins (`folio`, `grit`, `rg`) under
local-dev use, a non-issue for a server-side/isolated MCP endpoint.

### Option B — `nix bundle` single self-extracting executable

`nix bundle` packs a flake output's closure into one self-extracting
executable.

**Verdict:** ruled out on Darwin. The Nix manual states `nix bundle` "only
works on Linux"; the default bundlers (`NixOS/bundlers`, built on
`nix-bundle`/`arx`) fake a `/nix/store` at runtime via Linux
user-namespace bind-mounts, which macOS has no equivalent for. There is no
macOS bundler in the curated set. This cannot produce a single self-contained
*Darwin* executable.

### Option C — native standalone-moxin distribution (prior art, partial)

Independent of the container work, moxy already has substantial native /
Homebrew distribution machinery (issue #109, design +
impl plans dated 2026-04-13):

- `moxy serve-moxin <name>` and `moxy list-moxins` subcommands **exist** in
  `cmd/moxy/main.go` — serve a single moxin as a standalone stdio MCP server.
- `@BIN@` placeholder convention **exists** in the flake: moxin command fields
  use `@BIN@/<tool>`, substituted to the nix store path in nix builds, and
  (per the design) resolved at runtime to `<SourceDir>/bin` for unwrapped
  tarballs. Backwards-compatible: nix-built moxins have `@BIN@` already
  substituted, so the runtime fallback never fires for them.
- The design specifies `moxy-static` (`CGO_ENABLED=0`), `mkBrewMoxin` /
  `mkBrewBunMoxin` (unwrapped moxin builders), `brew-moxins`, `brew-tarball`,
  and an `install-moxin.bash` curl-installable script with a per-moxin brew
  dependency map.

**Verdict:** this is the path that aligns with "moxins are intentionally
ambient-scoped TOML that spawn whatever's on PATH." It distributes a native
static moxy + unwrapped moxins, with `brew`/`apt` filling external-tool deps.
**Status uncertain:** the impl plan was committed (`2f7e4c8`) but the named
flake outputs (`moxy-static`, `mkBrewMoxin`, `brew-tarball`) are **not present
under those names in the current flake** — only the `serve-moxin`/`list-moxins`
subcommands and the `@BIN@` convention are confirmed on the current tree. The
brew-build work appears partially landed or later refactored; its true state
must be re-verified before building on it.

### The Homebrew formula composition idea

A Homebrew formula for moxy could `depends_on "container"` (Option A) or
`depends_on "git"/"jq"/"ripgrep"/…` (Option C), folding the prerequisite
installs into one `brew install moxy`. For Option A this wraps the container;
for Option C it's brew distributing the native closure directly (brew's native
job). Note these are alternatives, not a stack — wrapping a container in a
formula carries two packaging systems where Option C needs one.

## Examples

Option A — the proven container loop (binary only):

    nix build .#moxy-oci-image
    container image load -i result
    container run --rm moxy:latest version
    # -> moxy 0.6.30+<commit> (printed from inside a linux/arm64 micro-VM)

    # or via the justfile recipe:
    just container-prototype

Option A — consumer side, once an image is published to a registry:

    brew install container
    container system start            # one-time; downloads a Linux kernel
    container run myregistry/moxy:latest version

Option C — intended consumer flow (per the #109 design, status unverified):

    curl -fsSL .../install-moxin.bash | bash -s -- grit
    # downloads static moxy + the grit moxin tarball, brew-installs git/jq,
    # registers: claude mcp add grit -- ~/.local/bin/moxy serve-moxin grit

## Limitations

- **Full image needs a Linux builder (moxy#386).** The container prototype
  ships only the moxy binary. The full `combined` package (moxy + all moxins +
  maneater) does not cross-compile from Darwin: `man-moxin` pulls in maneater
  (CGo + llama-cpp), and several moxins wrap coreutils/git/pandoc with
  cross-eval friction. A complete image requires a real `aarch64-linux` Nix
  builder (nixpkgs `linux-builder`, or Nix-in-a-Linux-VM registered as a
  remote builder).
- **maneater can't go single-file anywhere.** The `man` moxin's
  semantic-search depends on maneater (CGo llama-cpp + a GGUF model file).
  Option C's design excludes it / degrades gracefully (search/section/toc work
  without it); Option A can only include it via a Linux builder. A pure
  single-file native moxy cannot bundle it.
- **`chix` requires Nix; `jira` requires commercial `acli`** — both are
  excluded from any Nix-less distribution (per the #109 design).
- **Container moxins see the VM filesystem, not the host's.** Under Option A,
  file-touching moxins operate inside the micro-VM. Local-dev use (operating on
  the user's repo) needs cwd bind-mounted in and still hits path-translation
  and git-worktree-resolution edges; server-side/isolated MCP use is unaffected.
- **`nix bundle` is Linux-only** — cannot target Darwin (Option B).

## More Information

- moxy#109 — standalone moxin installer (the native/brew prior art):
  `docs/plans/2026-04-13-standalone-moxin-installer-design.md` and
  `…-impl.md`.
- moxy#386 — full moxy OCI image needs an aarch64-linux Nix builder.
- amarbel-llc/igloo#44 — gomod2nix cross-compile bug worked around by the
  container prototype's `moxy-linux` derivation (overrides `go.GOOS/GOARCH` on
  the native pkgs set rather than using `pkgsCross`).
- Container prototype: `flake.nix` (`moxy-linux` / `moxy-docker-image` /
  `moxy-oci-image`), `justfile` (`container-prototype` recipe).
- Apple `container`: https://github.com/apple/container — runs Linux guests as
  per-container micro-VMs; consumes/produces standard OCI Linux images;
  requires macOS 26 for full functionality (runs on macOS 15 with networking
  caveats).
