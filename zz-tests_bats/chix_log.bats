#!/usr/bin/env bats

# bats file_tags=chix

# Tests for chix.log (nix log wrapper). Covers #252.

load 'common'

BIN="${CHIX_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/chix/bin}"

# A nix stub that records its argv one-per-line to $HOME/nix-argv and
# returns canned log output.
NIX_STUB=""

setup() {
  setup_test_home

  NIX_STUB="$HOME/bin/nix"
  mkdir -p "$HOME/bin"
  # Note: no shebang — nix sandbox lacks /usr/bin/env.
  cat > "$NIX_STUB" <<'EOF'
set -euo pipefail
printf '%s\n' "$@" > "$HOME/nix-argv"
echo "fake build log output for testing"
EOF
  chmod +x "$NIX_STUB"

  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

@test "chix.log: passes 'log' subcommand and drv path to nix" {
  run "$BIN/log" "/nix/store/abc123-foo.drv"
  assert_success
  assert_output --partial "fake build log output"
  run cat "$HOME/nix-argv"
  assert_success
  assert_output --partial "log"
  assert_output --partial "/nix/store/abc123-foo.drv"
}

@test "chix.log: accepts an installable (not just a drv path)" {
  run "$BIN/log" ".#bats-default"
  assert_success
  run cat "$HOME/nix-argv"
  assert_success
  assert_output --partial "log"
  assert_output --partial ".#bats-default"
}

@test "chix.log: propagates nix failure" {
  # Overwrite stub to fail
  cat > "$NIX_STUB" <<'EOF'
set -euo pipefail
printf '%s\n' "$@" > "$HOME/nix-argv"
echo "error: derivation not found" >&2
exit 1
EOF
  chmod +x "$NIX_STUB"

  run "$BIN/log" "/nix/store/notfound.drv"
  assert_failure
}
