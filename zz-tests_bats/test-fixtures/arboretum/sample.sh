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

# A `case` statement exercises the bash grammar's external scanner — the
# construct that crashed the old vendored grammar (moxy#379).
dispatch() {
  case "$1" in
    shout) shout "$2" ;;
    greet) greet "$2" ;;
    *) main "$@" ;;
  esac
}

main "$@"
