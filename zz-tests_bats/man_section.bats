#!/usr/bin/env bats

# bats file_tags=man

load 'common'

BIN="${MAN_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/man/bin}"

setup() {
  setup_test_home
  # Install the pivy-tool.1 fixture into a local MANPATH so man -w can find it.
  mkdir -p "$HOME/man/man1"
  cp "$BATS_TEST_DIRNAME/test-fixtures/pivy-tool.1" "$HOME/man/man1/pivy-tool.1"
  export MANPATH="$HOME/man:"
}

teardown() {
  teardown_test_home
}

function man_toc_lists_both_top_level_sections_and_subsections { # @test
  run "$BIN/toc" "pivy-tool"
  assert_success
  # Top-level .SH sections
  assert_output --partial "# OPERATIONS"
  # .SS subsections
  assert_output --partial "## Informational"
  assert_output --partial "## Setup"
}

function man_section_resolves_a_top_level_SH_section { # @test
  run "$BIN/section" "pivy-tool" "NAME"
  assert_success
  assert_output --partial "pivy-tool"
}

function man_section_resolves_a_SS_subsection_by_name { # @test
  run "$BIN/section" "pivy-tool" "Informational"
  assert_success
  assert_output --partial ".SS"
}

function man_section_subsection_lookup_is_case_insensitive { # @test
  run "$BIN/section" "pivy-tool" "informational"
  assert_success
  assert_output --partial ".SS"
}

function man_section_error_message_lists_both_sections_and_subsections { # @test
  run "$BIN/section" "pivy-tool" "Nonexistent Section"
  assert_failure
  # Top-level section appears
  assert_output --partial "OPERATIONS"
  # Subsection appears (indented under its parent)
  assert_output --partial "Informational"
}
