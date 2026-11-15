#!/usr/bin/env bats

# bats file_tags=chix

# moxy#519: the free-form `args` of chix.flake-check, flake-lock and
# flake-update are checked before nix runs, so they cannot name a path or code
# the flake_dir stanza never saw. A refused argument never reaches nix: the nix
# stub records its argv, and a refusal leaves no record.

load 'common'

BIN="${CHIX_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/chix/bin}"

setup() {
  setup_test_home
  mkdir -p "$HOME/bin" "$HOME/flake"
  export PATH="$HOME/bin:$PATH"
  # No shebang on the stub — the nix sandbox lacks /usr/bin/env.
  cat >"$HOME/bin/nix" <<'EOF'
printf '%s\n' "$@" >"$HOME/nix-argv"
echo "stdout from nix"
EOF
  chmod +x "$HOME/bin/nix"
}

teardown() {
  teardown_test_home
}

assert_refused() { # TOOL ARGS-JSON
  run "$BIN/flake-$1" "$2" "$HOME/flake"
  assert_failure 1
  assert_output --partial "refusing"
  [[ ! -e $HOME/nix-argv ]] || fail "flake-$1 '$2' reached nix: $(<"$HOME/nix-argv")"
}

function path_and_code_bearing_flags_never_reach_nix { # @test
  local sub arg
  for sub in check lock update; do
    for arg in \
      '["--flake","/etc"]' \
      '["--flake=/etc"]' \
      '["--file","/etc/x.nix"]' \
      '["-f","/etc/x.nix"]' \
      '["--expr","1"]' \
      '["--override-input","nixpkgs","path:/etc"]' \
      '["--override-flake","a","path:/etc"]' \
      '["--reference-lock-file","/etc/passwd"]' \
      '["--output-lock-file","/tmp/x"]' \
      '["--inputs-from","/etc"]' \
      '["-I","/etc"]' \
      '["--include","/etc"]' \
      '["--option","post-build-hook","/tmp/x"]' \
      '["--post-build-hook","/tmp/x"]' \
      '["--extra-sandbox-paths","/etc"]' \
      '["--impure"]' \
      '["--command","id"]' \
      '["--"]' \
      '["-Lf","/etc/x.nix"]'; do
      assert_refused "$sub" "$arg"
    done
  done
}

function flake_references_other_than_dot_never_reach_nix_for_check_and_lock { # @test
  local sub arg
  for sub in check lock; do
    for arg in \
      '["/etc"]' \
      '["path:/etc"]' \
      '["../other"]' \
      '["./sub"]' \
      '["github:owner/repo"]' \
      '["git+file:///etc"]' \
      '["nixpkgs"]' \
      '["an arg with spaces"]'; do
      assert_refused "$sub" "$arg"
    done
  done
}

function only_input_names_reach_nix_for_update { # @test
  local arg
  for arg in \
    '["/etc"]' \
    '["path:/etc"]' \
    '["."]' \
    '["a/b"]' \
    '["an arg with spaces"]' \
    '["--update-input"]' \
    '["--update-input","/etc"]' \
    '["--update-input","a b"]'; do
    assert_refused update "$arg"
  done
}

function allowed_flags_and_names_reach_nix { # @test
  run "$BIN/flake-update" '["nixpkgs","my-input_2","--refresh","-L"]' "$HOME/flake"
  assert_success
  run cat "$HOME/nix-argv"
  assert_output "$(printf '%s\n' flake update nixpkgs my-input_2 --refresh -L)"

  run "$BIN/flake-update" '["--update-input","nixpkgs"]' "$HOME/flake"
  assert_success

  run "$BIN/flake-check" '["--keep-going","--no-build",".","-vv"]' "$HOME/flake"
  assert_success
  run cat "$HOME/nix-argv"
  assert_output "$(printf '%s\n' flake check --keep-going --no-build . -vv)"
}

function the_refusal_names_the_argument_and_exits_one { # @test
  run "$BIN/flake-check" '["--flake","/etc"]' "$HOME/flake"
  assert_failure 1
  assert_output --partial "chix flake check: refusing --flake"
}
