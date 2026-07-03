#!/usr/bin/env bats

# bats file_tags=chix

# Tests for chix.why-depends (nix why-depends wrapper, #380). Stubs `nix` and
# `nix-store` (the nix sandbox has no daemon), mirroring chix_log.bats.

load 'common'

BIN="${CHIX_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/chix/bin}"

setup() {
  setup_test_home
  mkdir -p "$HOME/bin"

  # nix stub: records the LAST invocation's argv; `path-info` returns a fake
  # drv (feeds bare-name resolution), `why-depends` returns a canned chain.
  # No shebang — the nix sandbox lacks /usr/bin/env.
  cat >"$HOME/bin/nix" <<'EOF'
set -euo pipefail
printf '%s\n' "$@" > "$HOME/nix-argv"
case "${1:-}" in
  path-info) echo "/nix/store/deadbeef-dependent.drv" ;;
  why-depends) echo "/nix/store/aaa-dependent -> /nix/store/bbb-ffmpeg-headless-8.0.1" ;;
esac
EOF
  chmod +x "$HOME/bin/nix"

  # nix-store stub: requisites listing including the bare-name match (.drv).
  cat >"$HOME/bin/nix-store" <<'EOF'
set -euo pipefail
printf '%s\n' "$@" > "$HOME/nix-store-argv"
printf '%s\n' \
  "/nix/store/aaa-dependent.drv" \
  "/nix/store/ccc-ffmpeg-headless-8.0.1.drv" \
  "/nix/store/ddd-other.drv"
EOF
  chmod +x "$HOME/bin/nix-store"

  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

function why_depends_passes_subcommand_and_paths { # @test
  run "$BIN/why-depends" "/nix/store/aaa-dependent" "/nix/store/bbb-ffmpeg"
  assert_success
  assert_output --partial "ffmpeg-headless"
  run cat "$HOME/nix-argv"
  assert_success
  assert_output --partial "why-depends"
  assert_output --partial "/nix/store/aaa-dependent"
  assert_output --partial "/nix/store/bbb-ffmpeg"
}

function why_depends_forwards_boolean_flags { # @test
  run "$BIN/why-depends" "/nix/store/aaa" "/nix/store/bbb" "true" "true" "true"
  assert_success
  run cat "$HOME/nix-argv"
  assert_success
  assert_output --partial "--derivation"
  assert_output --partial "--all"
  assert_output --partial "--precise"
}

function why_depends_omits_flags_when_false { # @test
  run "$BIN/why-depends" "/nix/store/aaa" "/nix/store/bbb"
  assert_success
  run cat "$HOME/nix-argv"
  assert_success
  refute_output --partial "--derivation"
  refute_output --partial "--all"
  refute_output --partial "--precise"
}

function why_depends_resolves_bare_name_in_derivation_mode { # @test
  # Bare name + derivation=true → resolved to the matching .drv from the
  # dependent's requisites, and THAT (not the bare name) is passed to nix.
  run "$BIN/why-depends" ".#devShells.default" "ffmpeg-headless" "true"
  assert_success
  run cat "$HOME/nix-argv"
  assert_success
  assert_output --partial "/nix/store/ccc-ffmpeg-headless-8.0.1.drv"
}

function why_depends_passes_installable_dependency_through { # @test
  # A '#'-bearing installable is used as-is (no bare-name resolution).
  run "$BIN/why-depends" ".#foo" "nixpkgs#hello"
  assert_success
  run cat "$HOME/nix-argv"
  assert_success
  assert_output --partial "nixpkgs#hello"
  # nix-store must not have been consulted for an installable dependency.
  assert [ ! -f "$HOME/nix-store-argv" ]
}

function why_depends_propagates_nix_failure { # @test
  cat >"$HOME/bin/nix" <<'EOF'
set -euo pipefail
echo "error: path is not in the closure" >&2
exit 1
EOF
  chmod +x "$HOME/bin/nix"
  run "$BIN/why-depends" "/nix/store/aaa" "/nix/store/bbb"
  assert_failure
}
