#!/usr/bin/env bats

# bats file_tags=chix

# `run !` (assert non-zero exit) is a bats 1.5.0+ feature; declare the baseline
# so bats doesn't emit BW02 ("using flags on run requires BATS_VERSION>=1.5.0").
bats_require_minimum_version 1.5.0

BIN="${CHIX_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/chix/bin}"

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

# flake-show on a non-existent path must fail AND surface the underlying
# nix error — not a raw bun/zx stack trace. Covers #260 and #270.
# bats's `run` merges stdout+stderr into $output, which is sufficient to
# verify the error message is surfaced (the wrapper writes the nix stderr
# to its own stderr, which bats captures here).
#
# `run !` asserts a non-zero exit without pinning the exact code: flake-show
# propagates nix's own exit status (127 here), which is not a stable contract
# across nix versions. `run !` also suppresses the BW01 "command not found"
# warning that a bare `run` emits on a 126/127 status.
function flake_show_surfaces_underlying_nix_error_not_bun_stack_trace { # @test
  run ! "$BIN/flake-show" "/nonexistent/not-a-flake"

  # Must NOT be a raw bun stack trace (lines like "bundled-file.js:NNN:NNN").
  if echo "$output" | grep -qE '\-bundle-[^/]+\.js:[0-9]+:[0-9]+'; then
    echo "Got bun stack trace instead of nix error: $output" >&2
    return 1
  fi

  # Output must be non-empty — the underlying nix error must be surfaced.
  [[ -n $output ]] || {
    echo "output was empty — underlying nix error was swallowed" >&2
    return 1
  }
}
