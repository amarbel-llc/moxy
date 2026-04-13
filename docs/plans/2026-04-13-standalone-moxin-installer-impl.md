# Standalone Moxin Installer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Enable `curl | bash` installation of individual moxins as standalone MCP servers.

**Architecture:** Nix builds static binary + per-moxin tarballs. A tag-triggered
GitHub Actions workflow publishes them as release artifacts. A bash install
script downloads, extracts, installs deps via brew, and registers with Claude
Code.

**Tech Stack:** Nix (build), GitHub Actions (CI), bash (install script), gum
(interactive UI)

**Rollback:** Delete the GitHub Release to remove all download artifacts.
`@BIN@` runtime resolution is a no-op fallback for nix-built moxins. No existing
systems are modified.

**Design doc:** `docs/plans/2026-04-13-standalone-moxin-installer-design.md`

---

### Task 1: Add sisyphus to brew-moxins

**Promotion criteria:** N/A

**Files:**
- Modify: `flake.nix:354-377` (brew-moxins attrset)

Sisyphus is a plain bash/python moxin (no TypeScript sources, no `src/`
directory). Uses `mkBrewMoxin`.

**Step 1: Add sisyphus to brew-moxins**

In `flake.nix`, add to the `brew-moxins` attrset after the `rg` entry:

```nix
sisyphus = mkBrewMoxin "sisyphus";
```

**Step 2: Build to verify**

Run: `nix build .#brew-tarball --show-trace`
Expected: builds successfully, tarball contains `share/moxy/moxins/sisyphus/`

**Step 3: Verify moxin contents**

```bash
tar -tzf result/moxy-*.tar.gz | grep sisyphus | head -5
```

Expected: `_moxin.toml`, tool TOMLs, and `bin/` scripts present.

**Step 4: Commit**

```bash
git add flake.nix
git commit -m "feat: add sisyphus to brew-moxins"
```

---

### Task 2: Add man to brew-moxins

**Promotion criteria:** N/A

**Files:**
- Modify: `flake.nix:354-377` (brew-moxins attrset)

Man is a plain bash moxin (no TypeScript). Uses `mkBrewMoxin`.

**Step 1: Add man to brew-moxins**

In `flake.nix`, add to the `brew-moxins` attrset:

```nix
man = mkBrewMoxin "man";
```

**Step 2: Build to verify**

Run: `nix build .#brew-tarball --show-trace`
Expected: builds successfully, tarball contains `share/moxy/moxins/man/`

**Step 3: Verify moxin contents**

```bash
tar -tzf result/moxy-*.tar.gz | grep '/man/' | head -10
```

Expected: `_moxin.toml`, tool TOMLs, and `bin/` scripts for search, section,
semantic-search, toc.

**Step 4: Commit**

```bash
git add flake.nix
git commit -m "feat: add man to brew-moxins"
```

---

### Task 3: Add just-us-agents to brew-moxins

**Promotion criteria:** N/A

**Files:**
- Modify: `flake.nix:354-377` (brew-moxins attrset)

just-us-agents has one TypeScript entrypoint (`list-recipes`) and one bash script
(`run-recipe`). The remaining tools call `just` directly. Uses `mkBrewBunMoxin`
because of the TypeScript source.

**Step 1: Add just-us-agents to brew-moxins**

In `flake.nix`, add to the `brew-moxins` attrset:

```nix
just-us-agents = mkBrewBunMoxin "just-us-agents" {
  "list-recipes" = "moxins/just-us-agents/src/list-recipes.ts";
};
```

**Step 2: Build to verify**

Run: `nix build .#brew-tarball --show-trace`
Expected: builds successfully

**Step 3: Verify moxin contents**

```bash
tar -tzf result/moxy-*.tar.gz | grep just-us-agents | head -10
```

Expected: `_moxin.toml`, tool TOMLs, `bin/list-recipes` (bun wrapper),
`bin/run-recipe` (bash script), `lib/list-recipes.js` (bundled JS).

**Step 4: Commit**

```bash
git add flake.nix
git commit -m "feat: add just-us-agents to brew-moxins"
```

---

### Task 4: Add per-moxin tarball nix outputs

**Promotion criteria:** N/A

**Files:**
- Modify: `flake.nix:379-418` (after brew-moxins, before packages output)

The combined `brew-tarball` serves Homebrew. Per-moxin tarballs serve the
`install-moxin.bash` script — each containing just one moxin directory.

**Step 1: Add standalone-moxin tarball builder**

After `brew-tarball` and before the `in` keyword, add a function and the
per-moxin derivations:

```nix
# Per-moxin tarballs for standalone install script.
# Each contains a single moxin directory suitable for extraction to
# ~/.local/share/moxy/moxins/<name>/
mkStandaloneMoxinTarball = name: drv: let
  arch = if pkgs.stdenv.hostPlatform.isAarch64 then "arm64"
         else "amd64";
  os = if pkgs.stdenv.isDarwin then "darwin" else "linux";
in pkgs.runCommand "${name}-moxin-tarball" {} ''
  staging=$TMPDIR/${name}
  cp -rL ${drv} $staging
  chmod -R u+w $staging
  mkdir -p $out
  tar -czf $out/${name}-moxin-${os}-${arch}.tar.gz -C $TMPDIR ${name}
'';

standalone-moxin-tarballs = builtins.mapAttrs mkStandaloneMoxinTarball brew-moxins;
```

**Step 2: Expose per-moxin tarballs in packages output**

In the `packages` attrset, add:

```nix
inherit standalone-moxin-tarballs;
```

This makes them accessible as `nix build .#standalone-moxin-tarballs.grit` etc.

**Step 3: Build a single per-moxin tarball to verify**

Run: `nix build .#standalone-moxin-tarballs.grit --show-trace`
Expected: `result/grit-moxin-darwin-arm64.tar.gz` (or equivalent for platform)

**Step 4: Verify tarball contents**

```bash
tar -tzf result/grit-moxin-*.tar.gz | head -10
```

Expected: `grit/_moxin.toml`, `grit/*.toml`, `grit/bin/*`

**Step 5: Build all per-moxin tarballs**

Run: `nix build .#standalone-moxin-tarballs.sisyphus .#standalone-moxin-tarballs.man .#standalone-moxin-tarballs.just-us-agents --show-trace`
Expected: all build successfully

**Step 6: Commit**

```bash
git add flake.nix
git commit -m "feat: add per-moxin standalone tarballs for install script"
```

---

### Task 5: Add `@BIN@` runtime resolution

**Promotion criteria:** N/A

**Files:**
- Modify: `internal/native/server.go:223` (before `exec.CommandContext`)
- Create: `internal/native/server_bin_test.go`

Moxin TOML configs use `@BIN@` placeholders in command fields. Nix builds
substitute these at build time. For standalone tarballs, `@BIN@` is left as-is
and needs runtime resolution to `<moxin-dir>/bin/`.

**Step 1: Write the failing test**

Create `internal/native/server_bin_test.go`:

```go
package native

import (
	"testing"
)

func TestResolveBinPlaceholder(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		sourceDir string
		want      string
	}{
		{
			name:      "resolves @BIN@ to sourceDir/bin",
			command:   "@BIN@/search",
			sourceDir: "/home/user/.local/share/moxy/moxins/grit",
			want:      "/home/user/.local/share/moxy/moxins/grit/bin/search",
		},
		{
			name:      "no placeholder unchanged",
			command:   "/nix/store/abc-grit/bin/search",
			sourceDir: "/home/user/.local/share/moxy/moxins/grit",
			want:      "/nix/store/abc-grit/bin/search",
		},
		{
			name:      "bare command unchanged",
			command:   "bash",
			sourceDir: "/some/dir",
			want:      "bash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveBinPlaceholder(tt.command, tt.sourceDir)
			if got != tt.want {
				t.Errorf("resolveBinPlaceholder(%q, %q) = %q, want %q",
					tt.command, tt.sourceDir, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/native/... -v -run TestResolveBinPlaceholder`
Expected: FAIL — `resolveBinPlaceholder` undefined

**Step 3: Add resolveBinPlaceholder function**

In `internal/native/server.go`, add near the top (after imports):

```go
// resolveBinPlaceholder replaces the @BIN@ placeholder in a tool command
// with the moxin's bin directory. This is a runtime fallback for standalone
// (non-nix) installs where @BIN@ was not substituted at build time.
func resolveBinPlaceholder(command, sourceDir string) string {
	if !strings.Contains(command, "@BIN@") {
		return command
	}
	return strings.ReplaceAll(command, "@BIN@", filepath.Join(sourceDir, "bin"))
}
```

Ensure `path/filepath` is in the imports (it should already be — check).

**Step 4: Run test to verify it passes**

Run: `go test ./internal/native/... -v -run TestResolveBinPlaceholder`
Expected: PASS

**Step 5: Wire into handleToolsCall**

In `internal/native/server.go`, at line 223, change:

```go
cmd := exec.CommandContext(ctx, spec.Command, allArgs...)
```

to:

```go
command := resolveBinPlaceholder(spec.Command, s.config.SourceDir)
cmd := exec.CommandContext(ctx, command, allArgs...)
```

**Step 6: Run all tests**

Run: `go test ./internal/native/... -v`
Expected: all pass

**Step 7: Run vet**

Run: `go vet ./...`
Expected: clean

**Step 8: Commit**

```bash
git add internal/native/server.go internal/native/server_bin_test.go
git commit -m "feat: add @BIN@ runtime resolution for standalone moxin installs"
```

---

### Task 6: Add release CI workflow

**Promotion criteria:** N/A

**Files:**
- Create: `.github/workflows/release.yml`

Tag-triggered workflow that builds static binaries and per-moxin tarballs on
both platforms, then creates a GitHub Release with all artifacts.

**Step 1: Create the workflow file**

Create `.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  build:
    strategy:
      matrix:
        include:
          - os: ubuntu-22.04
            system: x86_64-linux
            platform: linux-amd64
          - os: macos-15
            system: aarch64-darwin
            platform: darwin-arm64
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: DeterminateSystems/nix-installer-action@main
        with:
          determinate: true
      - uses: DeterminateSystems/flakehub-cache-action@main

      - name: Build static binary
        run: |
          nix build .#moxy-static
          mkdir -p artifacts
          cp result/bin/moxy artifacts/
          tar -czf artifacts/moxy-${{ matrix.platform }}.tar.gz -C artifacts moxy
          rm artifacts/moxy

      - name: Build per-moxin tarballs
        run: |
          for name in $(nix eval .#standalone-moxin-tarballs --apply 'x: builtins.concatStringsSep " " (builtins.attrNames x)' --raw); do
            nix build ".#standalone-moxin-tarballs.$name"
            cp result/*.tar.gz artifacts/
          done

      - uses: actions/upload-artifact@v4
        with:
          name: artifacts-${{ matrix.platform }}
          path: artifacts/

  release:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/download-artifact@v4
        with:
          path: artifacts
          merge-multiple: true

      - name: Create release
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          cp scripts/install-moxin.bash artifacts/
          gh release create "${{ github.ref_name }}" \
            --title "${{ github.ref_name }}" \
            --generate-notes \
            artifacts/*
```

**Step 2: Validate YAML syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))"`
Expected: no errors (or use `yq` if available)

**Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "feat: add tag-triggered release workflow for standalone moxin installer"
```

---

### Task 7: Write install-moxin.bash

**Promotion criteria:** N/A

**Files:**
- Create: `scripts/install-moxin.bash`

The curl-installable script. Downloads moxy binary + a chosen moxin tarball,
installs brew dependencies, and registers with Claude Code.

**Step 1: Create scripts directory and install script**

Create `scripts/install-moxin.bash`:

```bash
#!/usr/bin/env bash
set -euo pipefail

# install-moxin.bash — install a standalone moxy moxin
#
# Usage:
#   curl -fsSL .../install-moxin.bash | bash           # interactive menu
#   curl -fsSL .../install-moxin.bash | bash -s -- grit # direct install

REPO="amarbel-llc/moxy"
INSTALL_BIN="$HOME/.local/bin"
INSTALL_SHARE="$HOME/.local/share/moxy/moxins"

# Standalone-eligible moxins and their brew dependencies.
declare -A MOXIN_DEPS=(
  [env]=""
  [folio]="jq gawk"
  [folio-external]="jq gawk"
  [freud]="python3"
  [grit]="git jq"
  [jq]="jq"
  [rg]="ripgrep"
  [get-hubbed]="git gh jq"
  [get-hubbed-external]="git gh jq"
  [hamster]="go"
  [sisyphus]="python3"
  [just-us-agents]="just jq"
  [man]="pandoc mandoc"
)

# Bun-based moxins need bun at runtime.
declare -A MOXIN_NEEDS_BUN=(
  [get-hubbed]=1
  [get-hubbed-external]=1
  [hamster]=1
  [just-us-agents]=1
)

# Pip packages needed by moxins.
declare -A MOXIN_PIP_DEPS=(
  [sisyphus]="atlassian-python-api"
)

# Env vars that must be set for a moxin to function.
declare -A MOXIN_ENV_NOTES=(
  [sisyphus]="Requires: JIRA_URL, JIRA_USERNAME, JIRA_API_TOKEN"
  [get-hubbed]="Requires: GH_TOKEN or gh auth login"
  [get-hubbed-external]="Requires: GH_TOKEN or gh auth login"
)

ELIGIBLE_MOXINS=($(echo "${!MOXIN_DEPS[@]}" | tr ' ' '\n' | sort))

die() { echo "error: $*" >&2; exit 1; }

detect_platform() {
  local os arch
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$os" in
    darwin) os="darwin" ;;
    linux)  os="linux" ;;
    *)      die "unsupported OS: $os" ;;
  esac
  case "$arch" in
    arm64|aarch64) arch="arm64" ;;
    x86_64)        arch="amd64" ;;
    *)             die "unsupported architecture: $arch" ;;
  esac
  PLATFORM="${os}-${arch}"
}

ensure_gum() {
  if command -v gum &>/dev/null; then return; fi
  echo "Installing gum for interactive selection..."
  if [[ "$(uname -s)" == "Darwin" ]]; then
    brew install gum
  else
    # Direct binary install for Linux
    local tmp
    tmp=$(mktemp -d)
    curl -fsSL "https://github.com/charmbracelet/gum/releases/latest/download/gum_$(uname -s)_$(uname -m).tar.gz" \
      | tar -xz -C "$tmp"
    install -m 755 "$tmp/gum" "$INSTALL_BIN/gum"
    rm -rf "$tmp"
  fi
}

get_latest_tag() {
  curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | head -1 | cut -d'"' -f4
}

download_and_extract() {
  local url="$1" dest="$2" desc="$3"
  echo "Downloading $desc..."
  local tmp
  tmp=$(mktemp -d)
  curl -fsSL "$url" | tar -xz -C "$tmp"
  mkdir -p "$dest"
  cp -r "$tmp"/* "$dest"/
  rm -rf "$tmp"
}

install_deps() {
  local name="$1"
  local deps="${MOXIN_DEPS[$name]:-}"
  local needs_bun="${MOXIN_NEEDS_BUN[$name]:-}"
  local pip_deps="${MOXIN_PIP_DEPS[$name]:-}"

  if [[ -n "$needs_bun" ]]; then
    deps="$deps bun"
  fi

  if [[ -z "$deps" && -z "$pip_deps" ]]; then return; fi

  local missing=()
  for dep in $deps; do
    local check_cmd="$dep"
    # Some brew package names differ from binary names.
    case "$dep" in
      ripgrep)   check_cmd="rg" ;;
      python3)   check_cmd="python3" ;;
    esac
    if ! command -v "$check_cmd" &>/dev/null; then
      missing+=("$dep")
    fi
  done

  if [[ ${#missing[@]} -gt 0 ]]; then
    echo "Installing dependencies: ${missing[*]}"
    if [[ "$(uname -s)" == "Darwin" ]]; then
      brew install "${missing[@]}"
    else
      # Best-effort: try apt if available.
      if command -v apt-get &>/dev/null; then
        sudo apt-get install -y "${missing[@]}"
      else
        die "missing dependencies (${missing[*]}) — install them manually"
      fi
    fi
  fi

  if [[ -n "$pip_deps" ]]; then
    echo "Installing pip packages: $pip_deps"
    pip3 install --user $pip_deps
  fi
}

register_moxin() {
  local name="$1"
  if ! command -v claude &>/dev/null; then
    echo "Claude Code not found on PATH — skipping registration."
    echo "Register manually:"
    echo "  claude mcp add $name -- $INSTALL_BIN/moxy serve-moxin $name"
    return
  fi
  echo "Registering $name with Claude Code..."
  claude mcp add "$name" \
    -e "MOXIN_PATH=$INSTALL_SHARE" \
    -- "$INSTALL_BIN/moxy" serve-moxin "$name"
}

main() {
  detect_platform

  local name="${1:-}"
  if [[ -z "$name" ]]; then
    ensure_gum
    name=$(printf '%s\n' "${ELIGIBLE_MOXINS[@]}" | gum choose --header "Select a moxin to install:")
    [[ -n "$name" ]] || die "no moxin selected"
  fi

  # Validate moxin name.
  if [[ -z "${MOXIN_DEPS[$name]+x}" ]]; then
    die "unknown moxin: $name (available: ${ELIGIBLE_MOXINS[*]})"
  fi

  local tag
  tag=$(get_latest_tag)
  [[ -n "$tag" ]] || die "could not determine latest release"
  local base_url="https://github.com/$REPO/releases/download/$tag"

  # Download and install moxy binary.
  echo "Installing moxy $tag..."
  mkdir -p "$INSTALL_BIN"
  download_and_extract "$base_url/moxy-$PLATFORM.tar.gz" "$INSTALL_BIN" "moxy binary"
  chmod +x "$INSTALL_BIN/moxy"

  # Download and install moxin.
  download_and_extract "$base_url/$name-moxin-$PLATFORM.tar.gz" "$INSTALL_SHARE" "$name moxin"

  # Install dependencies.
  install_deps "$name"

  # Register with Claude Code.
  register_moxin "$name"

  echo ""
  echo "Installed: $name"
  echo "  Binary:  $INSTALL_BIN/moxy"
  echo "  Moxin:   $INSTALL_SHARE/$name/"

  local notes="${MOXIN_ENV_NOTES[$name]:-}"
  if [[ -n "$notes" ]]; then
    echo ""
    echo "  $notes"
  fi

  # PATH hint.
  if [[ ":$PATH:" != *":$INSTALL_BIN:"* ]]; then
    echo ""
    echo "Add $INSTALL_BIN to your PATH:"
    echo "  export PATH=\"$INSTALL_BIN:\$PATH\""
  fi
}

main "$@"
```

**Step 2: Make executable**

```bash
chmod +x scripts/install-moxin.bash
```

**Step 3: Verify syntax**

Run: `bash -n scripts/install-moxin.bash`
Expected: no errors

**Step 4: Commit**

```bash
git add scripts/install-moxin.bash
git commit -m "feat: add curl-installable moxin install script"
```

---

### Task 8: Smoke test end-to-end

**Promotion criteria:** N/A

**Files:** (none modified)

Verify the nix build produces all expected artifacts.

**Step 1: Build static binary**

Run: `nix build .#moxy-static --show-trace`
Expected: `result/bin/moxy` exists and is statically linked

**Step 2: Build all per-moxin tarballs**

Run: `nix build .#standalone-moxin-tarballs.grit --show-trace`
Expected: tarball in `result/`

**Step 3: Test serve-moxin with a standalone moxin**

Extract a per-moxin tarball and point `MOXIN_PATH` at it:

```bash
tmp=$(mktemp -d)
tar -xzf result/grit-moxin-*.tar.gz -C "$tmp"
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test"}}}' \
  | MOXIN_PATH="$tmp" ./result/bin/moxy serve-moxin grit 2>/dev/null \
  | head -1 | jq .result.serverInfo
rm -rf "$tmp"
```

Expected: JSON with `name: "grit"` — confirms `@BIN@` runtime resolution works
with the standalone tarball layout.

**Step 4: Run full test suite**

Run: `just test-go`
Expected: all tests pass

**Step 5: Run bats tests**

Run: `just test-bats`
Expected: all tests pass
