#!/usr/bin/env bash
# Sample bash script.

set -euo pipefail

DEFAULT_NAME="world"

shout() {
  local s="$1"
  echo "${s^^}"
}

greet() {
  local name="${1:-$DEFAULT_NAME}"
  echo "hi, $name"
}

main() {
  shout "$(greet "$@")"
}

main "$@"
