#!/usr/bin/env bats

# bats file_tags=chix

BIN="${CHIX_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/chix/bin}"

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

# flake-show on a non-existent path must fail AND surface the underlying
# nix error — not a raw bun/zx stack trace.
# bats's `run` merges stdout+stderr into $output, which is sufficient to
# verify the error message is surfaced (the wrapper writes the nix stderr
# to its own stderr, which bats captures here).
function flake_show_surfaces_underlying_nix_error_not_bun_stack_trace { # @test
  run "$BIN/flake-show" "/nonexistent/not-a-flake"
  assert_failure

  # Must NOT be a raw bun stack trace (lines like "bundled-file.js:NNN:NNN").
  if echo "$output" | grep -qE '\-bundle-[^/]+\.js:[0-9]+:[0-9]+'; then
    echo "Got bun stack trace instead of nix error: $output" >&2
    return 1
  fi

  # Output must be non-empty — the underlying nix error must be surfaced.
  [[ -n "$output" ]] || {
    echo "output was empty — underlying nix error was swallowed" >&2
    return 1
  }
}
