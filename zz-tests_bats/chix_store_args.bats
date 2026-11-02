#!/usr/bin/env bats

# bats file_tags=chix

# moxy#495: chix's always-allow store tools (log, derivation-show,
# store-path-info, store-cat, store-ls) cannot be steered at a file outside the
# store. A rejected argument never reaches nix: the nix stub records its argv,
# and a rejection leaves no record.

load 'common'

BIN="${CHIX_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/chix/bin}"

setup() {
  setup_test_home

  mkdir -p "$HOME/bin"
  # Note: no shebang — nix sandbox lacks /usr/bin/env.
  cat >"$HOME/bin/nix" <<'EOF'
set -euo pipefail
printf '%s\n' "$@" > "$HOME/nix-argv"
echo '{"fake":"derivation","env":{"k":"v"}}'
EOF
  chmod +x "$HOME/bin/nix"
  export PATH="$HOME/bin:$PATH"

  mkdir -p "$HOME/work"
  cd "$HOME/work"
  ln -s /etc/passwd "$HOME/work/escape"
  STORE_FILE=$(readlink -f "$(command -v bash)")
  STORE_DIR=$(dirname "$STORE_FILE")
}

teardown() {
  teardown_test_home
}

STORE_TOOLS="log derivation-show store-path-info"

assert_rejected() { # TOOL ARG
  run "$BIN/$1" "$2"
  assert_failure
  [[ ! -e $HOME/nix-argv ]] || fail "$1 '$2' reached nix: $(<"$HOME/nix-argv")"
}

function filesystem_paths_outside_the_store_never_reach_nix { # @test
  local t arg
  for t in $STORE_TOOLS; do
    for arg in \
      /etc/passwd \
      /nix/store/../etc/passwd \
      /nix/store/x/../../../etc/passwd \
      /nix/store//../etc/passwd \
      /nix/storeevil/x \
      /nix/store \
      "$HOME/work/escape" \
      ./escape \
      escape \
      ../work/escape \
      "~/.ssh/id_rsa" \
      "%2e%2e/etc/passwd" \
      "./%2e%2e/%2e%2e/etc/passwd"; do
      assert_rejected "$t" "$arg"
    done
  done
}

function nix_options_and_non_store_references_never_reach_nix { # @test
  local t arg
  for t in $STORE_TOOLS; do
    for arg in \
      -f \
      --file \
      "--option" \
      "-L" \
      "path:/etc" \
      "path:." \
      "git+file:///etc" \
      "file:///etc/passwd" \
      "tarball+file:///etc/passwd" \
      ".#x/../../y" \
      ".#a b" \
      "nixpkgs#hello;id" \
      ""; do
      assert_rejected "$t" "$arg"
    done
  done
}

function control_characters_and_oversize_input_never_reach_nix { # @test
  local t
  for t in $STORE_TOOLS; do
    assert_rejected "$t" $'/nix/store/x\n/etc/passwd'
    assert_rejected "$t" $'/nix/store/x\t'
    assert_rejected "$t" "/nix/store/$(printf 'a%.0s' $(seq 5000))"
  done
}

function store_paths_installables_and_store_symlinks_reach_nix { # @test
  local t
  for t in $STORE_TOOLS; do
    for arg in \
      /nix/store/abc123-foo.drv \
      "/nix/store/abc123-foo.drv^out" \
      . \
      .#bats-default \
      '.#packages.x86_64-linux."a.b"' \
      nixpkgs#hello \
      github:owner/repo#pkg \
      github:owner/repo/abc123#pkg; do
      rm -f "$HOME/nix-argv"
      run "$BIN/$t" "$arg"
      assert_success
      run cat "$HOME/nix-argv"
      assert_output --partial "$arg"
    done
  done
}

function a_symlink_into_the_store_is_resolved_to_the_store_path { # @test
  [[ $STORE_DIR == /nix/store/* ]] || skip "bash is not in /nix/store here"
  ln -s "$STORE_DIR" "$HOME/work/result"
  local t
  for t in $STORE_TOOLS; do
    rm -f "$HOME/nix-argv"
    run "$BIN/$t" result
    assert_success
    run cat "$HOME/nix-argv"
    assert_output --partial "$STORE_DIR"
    refute_output --partial "result"
  done
}

function a_store_path_with_percent_escapes_stays_a_literal_store_name { # @test
  # %2e%2e is a literal directory name inside the store, not '..'
  rm -f "$HOME/nix-argv"
  run "$BIN/log" "/nix/store/%2e%2e/etc/passwd"
  assert_success
  run cat "$HOME/nix-argv"
  assert_output --partial "/nix/store/%2e%2e/etc/passwd"
}

function derivation_show_runs_its_filter_without_the_environment { # @test
  export SECRET_FOR_TEST=hunter2
  run "$BIN/derivation-show" /nix/store/abc123-foo.drv 'env | keys | length'
  assert_success
  assert_output "0"
  run "$BIN/derivation-show" /nix/store/abc123-foo.drv '$ENV.SECRET_FOR_TEST'
  assert_success
  assert_output "null"
}

function derivation_show_treats_a_filter_that_looks_like_an_option_as_a_filter { # @test
  run "$BIN/derivation-show" /nix/store/abc123-foo.drv -n
  assert_failure
  refute_output --partial "fake"
  run "$BIN/derivation-show" /nix/store/abc123-foo.drv '.fake'
  assert_success
  assert_output '"derivation"'
}

function store_cat_reads_only_regular_files_that_resolve_into_the_store { # @test
  local arg
  for arg in /etc/passwd /nix/store/../etc/passwd "$HOME/work/escape" ./escape escape \
    "/nix/store/x/../../etc/passwd" "%2e%2e/etc/passwd" -v --help; do
    run "$BIN/store-cat" "$arg"
    assert_failure
    refute_output --partial "root:"
  done
}

function store_ls_lists_only_directories_that_resolve_into_the_store { # @test
  local arg
  for arg in /etc /nix/store/../etc "$HOME" "$HOME/work" ".." "/" ./escape escape; do
    run "$BIN/store-ls" "$arg"
    assert_failure
  done
  [[ $STORE_DIR == /nix/store/* ]] || skip "bash is not in /nix/store here"
  run "$BIN/store-ls" "$STORE_DIR"
  assert_success
  assert_output --partial '"name":"bash"'
}
