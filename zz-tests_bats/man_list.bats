#!/usr/bin/env bats

# bats file_tags=man

# moxy#176: man.list lists the pages of every MANPATH directory, including one
# no man-db index covers (a nix profile, a store path), not only what
# `man -k` finds in an index.

load 'common'

bats_require_minimum_version 1.5.0

BIN="${MAN_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/man/bin}"

setup() {
  setup_test_home
  # No mandb has run here: the directory has no index.
  mkdir -p "$HOME/man/man1"
  cp "$BATS_TEST_DIRNAME/test-fixtures/pivy-tool.1" "$HOME/man/man1/pivy-tool.1"
  gzip -c "$BATS_TEST_DIRNAME/test-fixtures/pivy-tool.1" >"$HOME/man/man1/pivy-gz.1.gz"
  printf '.TH BARE 1\n.SH SYNOPSIS\nbare\n' >"$HOME/man/man1/bare.1"
  export MANPATH="$HOME/man"
}

teardown() {
  teardown_test_home
}

function an_unindexed_page_is_listed_with_its_description { # @test
  run "$BIN/list"
  assert_success
  assert_output --partial "pivy-tool - perform operations on PIV tokens and smart cards"
}

function a_compressed_page_is_listed_by_its_page_name { # @test
  run "$BIN/list"
  assert_success
  assert_line --regexp '^pivy-gz( |$)'
}

function a_page_with_no_name_line_is_listed_by_name_alone { # @test
  run "$BIN/list"
  assert_success
  assert_line "bare"
}
