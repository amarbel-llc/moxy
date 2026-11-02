#!/usr/bin/env bats

# bats file_tags=man

# moxy#495: man's always-allow tools read only manual pages resolved through
# MANPATH from a page name. A path, an option, '..', a control character or an
# oversize name is refused before man(1) sees it (man formats any argument that
# contains a slash as a file).

load 'common'

bats_require_minimum_version 1.5.0

BIN="${MAN_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/man/bin}"

setup() {
  setup_test_home
  mkdir -p "$HOME/man/man1"
  cp "$BATS_TEST_DIRNAME/test-fixtures/pivy-tool.1" "$HOME/man/man1/pivy-tool.1"
  export MANPATH="$HOME/man:"

  # A roff file outside MANPATH whose heading must never be readable by name.
  mkdir -p "$HOME/outside"
  printf '.SH SECRETS\nthe-secret-body\n' >"$HOME/outside/secret.1"
  ln -s "$HOME/outside/secret.1" "$HOME/man/man1/linked.1"

  BAD_NAMES=(
    "$HOME/outside/secret.1"
    "/etc/passwd"
    "./secret.1"
    "../outside/secret.1"
    "man1/../../outside/secret.1"
    "..%2f..%2foutside%2fsecret.1"
    "pivy-tool/../../outside/secret.1"
    "-C"
    "--help"
    "-M/tmp"
    ".hidden"
    "..."
    "pivy-tool(1"
    "pivy-tool(1)x"
    "pivy-tool (1)"
    "pivy-tool;id"
    'pivy-tool$(id)'
    ""
  )
}

teardown() {
  teardown_test_home
}

function toc_and_section_refuse_names_that_are_not_page_names { # @test
  local name
  for name in "${BAD_NAMES[@]}"; do
    run "$BIN/toc" "$name"
    assert_failure
    refute_output --partial "SECRETS"
    refute_output --partial "the-secret-body"
    run "$BIN/section" "$name" "SECRETS"
    assert_failure
    refute_output --partial "the-secret-body"
  done
}

function control_characters_and_oversize_names_are_refused { # @test
  local name
  for name in $'pivy-tool\n/etc/passwd' $'pivy-tool\t' $'pivy-tool\r' \
    "$(printf 'a%.0s' $(seq 300))" "pivy-tool($(printf '1%.0s' $(seq 40)))"; do
    run "$BIN/toc" "$name"
    assert_failure
    run "$BIN/section" "$name" "NAME"
    assert_failure
  done
}

function a_nul_byte_cannot_reach_the_tools { # @test
  # bash cannot pass NUL in argv; the tool sees the name cut at the NUL
  run "$BIN/toc" $'pivy-tool\0../outside/secret.1'
  refute_output --partial "SECRETS"
}

function real_page_names_still_resolve { # @test
  run "$BIN/toc" "pivy-tool"
  assert_success
  assert_output --partial "# OPERATIONS"
  run "$BIN/toc" "pivy-tool(1)"
  assert_success
  run "$BIN/section" "pivy-tool(1)" "NAME"
  assert_success
}

function a_symlinked_page_inside_manpath_is_still_only_a_page { # @test
  # the name is what is validated; the file man resolves for a valid name is
  # whatever MANPATH holds, as for any man page
  run "$BIN/toc" "linked"
  assert_success
  assert_output --partial "# SECRETS"
}

function the_section_title_is_data_not_a_path { # @test
  run "$BIN/section" "pivy-tool" "../../../etc/passwd"
  assert_failure
  refute_output --partial "root:"
  run "$BIN/section" "pivy-tool" "$(printf 'x%.0s' $(seq 5000))"
  assert_failure
}
