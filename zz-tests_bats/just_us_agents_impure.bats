#!/usr/bin/env bats

# bats file_tags=just_us_agents

# Tests that run-recipe passes --impure to nix develop when
# JUST_US_AGENTS_IMPURE=1 is set. Covers #268.

load 'common'

BIN="${JUST_US_AGENTS_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/just-us-agents/bin}"

setup() {
  setup_test_home

  # Create a fake flake.nix so the script takes the nix-develop branch.
  mkdir -p "$HOME/project"
  touch "$HOME/project/flake.nix"

  # Stub nix: records argv one-per-line to $HOME/nix-argv, then exec just
  # so the recipe actually runs.
  mkdir -p "$HOME/bin"
  # Note: no shebang — the nix sandbox lacks /usr/bin/env.
  cat >"$HOME/bin/nix" <<'STUB'
set -euo pipefail
printf '%s\n' "$@" > "$HOME/nix-argv"
# shift past "develop [--impure] -c" to find "just <args>"
while [ "$#" -gt 0 ] && [ "$1" != "-c" ]; do shift; done
shift  # drop "-c"
exec "$@"
STUB
  chmod +x "$HOME/bin/nix"

  # Stub just: records argv one-per-line to $HOME/just-argv and exits 0.
  cat >"$HOME/bin/just" <<'STUB'
set -euo pipefail
printf '%s\n' "$@" > "$HOME/just-argv"
STUB
  chmod +x "$HOME/bin/just"

  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

function run_recipe_pure_mode_does_NOT_pass_impure_to_nix_develop { # @test
  cd "$HOME/project"
  unset JUST_US_AGENTS_IMPURE || true
  run "$BIN/run-recipe" "build" "" "" ""
  assert_success
  run cat "$HOME/nix-argv"
  assert_success
  assert_output --partial "develop"
  refute_output --partial "--impure"
}

function run_recipe_JUST_US_AGENTS_IMPURE_1_passes_impure_to_nix_develop { # @test
  cd "$HOME/project"
  JUST_US_AGENTS_IMPURE=1 run "$BIN/run-recipe" "build" "" "" ""
  assert_success
  run cat "$HOME/nix-argv"
  assert_success
  assert_output --partial "develop"
  assert_output --partial "--impure"
}

function run_recipe_no_flake_nix_skips_nix_develop_entirely { # @test
  cd "$HOME"                                                  # no flake.nix here
  JUST_US_AGENTS_IMPURE=1 run "$BIN/run-recipe" "build" "" "" ""
  assert_success
  # nix was never called — nix-argv file should not exist
  run test -f "$HOME/nix-argv"
  assert_failure
}
