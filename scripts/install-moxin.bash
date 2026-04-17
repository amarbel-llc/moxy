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
  ["env"]=""
  ["folio"]="jq gawk"
  ["folio-external"]="jq gawk"
  ["freud"]="python3"
  ["grit"]="git jq"
  ["jq"]="jq"
  ["rg"]="ripgrep"
  ["get-hubbed"]="git gh jq"
  ["get-hubbed-external"]="git gh jq"
  ["hamster"]="go"
  ["sisyphus"]="python3"
  ["just-us-agents"]="just jq"
  ["man"]="pandoc mandoc"
  ["car"]=""
  ["piers"]=""
  ["prison"]=""
  ["gmail"]=""
  ["calendar"]=""
  ["gws"]=""
  ["slip"]=""
)

# Bun-based moxins need bun at runtime.
declare -A MOXIN_NEEDS_BUN=(
  ["get-hubbed"]=1
  ["get-hubbed-external"]=1
  ["hamster"]=1
  ["just-us-agents"]=1
  ["car"]=1
  ["piers"]=1
  ["prison"]=1
  ["gmail"]=1
  ["calendar"]=1
  ["gws"]=1
)

# Moxins that need the Google Workspace CLI (gws) binary.
declare -A MOXIN_NEEDS_GWS=(
  ["car"]=1
  ["piers"]=1
  ["prison"]=1
  ["gmail"]=1
  ["calendar"]=1
  ["gws"]=1
)

# Pip packages needed by moxins.
declare -A MOXIN_PIP_DEPS=(
  ["sisyphus"]="atlassian-python-api"
)

# Env vars that must be set for a moxin to function.
declare -A MOXIN_ENV_NOTES=(
  ["sisyphus"]="Requires: JIRA_URL, JIRA_USERNAME, JIRA_API_TOKEN"
  ["get-hubbed"]="Requires: GH_TOKEN or gh auth login"
  ["get-hubbed-external"]="Requires: GH_TOKEN or gh auth login"
  ["car"]="Requires: gws auth login (Google OAuth)"
  ["piers"]="Requires: gws auth login (Google OAuth)"
  ["prison"]="Requires: gws auth login (Google OAuth)"
  ["gmail"]="Requires: gws auth login (Google OAuth)"
  ["calendar"]="Requires: gws auth login (Google OAuth)"
  ["gws"]="Requires: gws auth login (Google OAuth)"
  ["slip"]="Requires: gws auth login (Google OAuth)"
)

ELIGIBLE_MOXINS=($(echo "${!MOXIN_DEPS[@]}" | tr ' ' '\n' | sort))

die() {
  echo "error: $*" >&2
  exit 1
}

detect_platform() {
  local os arch
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$os" in
  darwin) os="darwin" ;;
  linux) os="linux" ;;
  *) die "unsupported OS: $os" ;;
  esac
  case "$arch" in
  arm64 | aarch64) arch="arm64" ;;
  x86_64) arch="amd64" ;;
  *) die "unsupported architecture: $arch" ;;
  esac
  PLATFORM="${os}-${arch}"
}

ensure_gum() {
  if command -v gum &>/dev/null; then return; fi
  echo "Installing gum for interactive selection..."
  if [[ "$(uname -s)" == "Darwin" ]]; then
    brew install gum
  else
    local tmp
    tmp=$(mktemp -d)
    curl -fsSL "https://github.com/charmbracelet/gum/releases/latest/download/gum_$(uname -s)_$(uname -m).tar.gz" |
      tar -xz -C "$tmp"
    install -m 755 "$tmp/gum" "$INSTALL_BIN/gum"
    rm -rf "$tmp"
  fi
}

GWS_VERSION="0.22.5"

ensure_gws() {
  if command -v gws &>/dev/null; then return; fi
  echo "Installing gws (Google Workspace CLI) v${GWS_VERSION}..."
  local os arch platform
  os=$(uname -s)
  arch=$(uname -m)
  case "${os}-${arch}" in
  Darwin-arm64)   platform="aarch64-apple-darwin" ;;
  Darwin-x86_64)  platform="x86_64-apple-darwin" ;;
  Linux-aarch64)  platform="aarch64-unknown-linux-gnu" ;;
  Linux-x86_64)   platform="x86_64-unknown-linux-gnu" ;;
  *) die "unsupported platform for gws: ${os}-${arch}" ;;
  esac
  local tmp
  tmp=$(mktemp -d)
  curl -fsSL "https://github.com/googleworkspace/cli/releases/download/v${GWS_VERSION}/google-workspace-cli-${platform}.tar.gz" |
    tar -xz -C "$tmp"
  install -m 755 "$tmp/gws" "$INSTALL_BIN/gws"
  rm -rf "$tmp"
}

get_latest_tag() {
  curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
    grep '"tag_name"' | head -1 | cut -d'"' -f4
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

  if [[ -n $needs_bun ]]; then
    deps="$deps bun"
  fi

  if [[ -z $deps && -z $pip_deps ]]; then return; fi

  local missing=()
  for dep in $deps; do
    local check_cmd="$dep"
    case "$dep" in
    ripgrep) check_cmd="rg" ;;
    python3) check_cmd="python3" ;;
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
      if command -v apt-get &>/dev/null; then
        sudo apt-get install -y "${missing[@]}"
      else
        die "missing dependencies (${missing[*]}) — install them manually"
      fi
    fi
  fi

  if [[ -n $pip_deps ]]; then
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
  if [[ -z $name ]]; then
    ensure_gum
    name=$(printf '%s\n' "${ELIGIBLE_MOXINS[@]}" | gum choose --header "Select a moxin to install:")
    [[ -n $name ]] || die "no moxin selected"
  fi

  # Validate moxin name.
  if [[ -z ${MOXIN_DEPS[$name]+x} ]]; then
    die "unknown moxin: $name (available: ${ELIGIBLE_MOXINS[*]})"
  fi

  local tag
  tag=$(get_latest_tag)
  [[ -n $tag ]] || die "could not determine latest release"
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

  # Install gws if needed.
  if [[ -n ${MOXIN_NEEDS_GWS[$name]:-} ]]; then
    ensure_gws
  fi

  # Register with Claude Code.
  register_moxin "$name"

  echo ""
  echo "Installed: $name"
  echo "  Binary:  $INSTALL_BIN/moxy"
  echo "  Moxin:   $INSTALL_SHARE/$name/"

  local notes="${MOXIN_ENV_NOTES[$name]:-}"
  if [[ -n $notes ]]; then
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
