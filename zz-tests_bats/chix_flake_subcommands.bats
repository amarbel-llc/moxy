#!/usr/bin/env bats

# bats file_tags=chix

# chix.flake-check, flake-lock and flake-update share one runner (#195):
# each runs `nix flake <subcommand> <args...>` in the flake directory, with
# every element of the JSON args array as one argv entry, and reports
# nix's merged output with exit 0 or 1.

load 'common'

BIN="${CHIX_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/chix/bin}"

setup() {
  setup_test_home
  mkdir -p "$HOME/bin" "$HOME/flake"
  export PATH="$HOME/bin:$PATH"
  # No shebang on the stub — the nix sandbox lacks /usr/bin/env.
  cat >"$HOME/bin/nix" <<'EOF'
printf '%s\n' "$PWD" >"$HOME/nix-cwd"
printf '%s\n' "$@" >"$HOME/nix-argv"
echo "stdout from nix"
echo "stderr from nix" >&2
exit "${NIX_STUB_EXIT:-0}"
EOF
  chmod +x "$HOME/bin/nix"
}

teardown() {
  teardown_test_home
}

function each_tool_runs_its_subcommand_with_each_arg_as_one_entry { # @test
  local sub
  for sub in check lock update; do
    run "$BIN/flake-$sub" '["--keep-going","an arg with spaces"]' "$HOME/flake"
    assert_success
    assert_output --partial "stdout from nix"
    assert_output --partial "stderr from nix"
    run cat "$HOME/nix-argv"
    assert_output "$(printf '%s\n' flake "$sub" --keep-going "an arg with spaces")"
    run cat "$HOME/nix-cwd"
    assert_output "$HOME/flake"
  done
}

function no_or_malformed_args_run_the_bare_subcommand { # @test
  local args
  for args in "" "null" "not json" '{"a":1}'; do
    run "$BIN/flake-check" "$args" "$HOME/flake"
    assert_success
    run cat "$HOME/nix-argv"
    assert_output "$(printf '%s\n' flake check)"
  done
}

function a_nix_failure_exits_one_with_its_output { # @test
  export NIX_STUB_EXIT=3
  run "$BIN/flake-lock" '[]' "$HOME/flake"
  assert_failure 1
  assert_output --partial "stderr from nix"
}
