#!/usr/bin/env bats

# bats file_tags=man

load 'common'

bats_require_minimum_version 1.5.0

BIN="${MAN_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/man/bin}"

setup() {
  setup_test_home
  # Install the pivy-tool.1 fixture into a local MANPATH so man -w can find it.
  mkdir -p "$HOME/man/man1" "$HOME/man/man7"
  cp "$BATS_TEST_DIRNAME/test-fixtures/pivy-tool.1" "$HOME/man/man1/pivy-tool.1"
  cp "$BATS_TEST_DIRNAME/test-fixtures/hyphen-heading.7" "$HOME/man/man7/hyphen-heading.7"
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

# moxy#446: headings written with roff-escaped hyphens (\-) must be
# reachable by the escaped title, the plain-hyphen title, and any case.
function man_section_opens_SS_with_escaped_hyphen_as_toc_lists_it { # @test
  run "$BIN/section" "hyphen-heading(7)" 'The escape hatch: goRun, ingest and godyn\-go'
  assert_success
  assert_output --partial "Subsection body with an escaped hyphen."
}

function man_section_opens_SS_with_plain_hyphen { # @test
  run "$BIN/section" "hyphen-heading(7)" "The escape hatch: goRun, ingest and godyn-go"
  assert_success
  assert_output --partial "Subsection body with an escaped hyphen."
}

function man_section_opens_hyphenated_all_caps_plain { # @test
  run "$BIN/section" "hyphen-heading(7)" "THE GENERATE-COMMIT-EVALUATE WORKFLOW"
  assert_success
  assert_output --partial "Hyphenated all-caps section body."
}

function man_section_opens_hyphenated_all_caps_escaped_without_awk_warning { # @test
  run --separate-stderr "$BIN/section" "hyphen-heading(7)" 'THE GENERATE\-COMMIT\-EVALUATE WORKFLOW'
  assert_success
  assert_output --partial "Hyphenated all-caps section body."
  refute [[ "$stderr" == *"escape sequence"* ]]
}

function man_section_opens_arrow_and_parens_heading_both_spellings { # @test
  run "$BIN/section" "hyphen-heading(7)" "CROSS-MODULE COMPOSITION (godyn → godyn)"
  assert_success
  assert_output --partial "Heading with an arrow and parentheses."
  run "$BIN/section" "hyphen-heading(7)" 'CROSS\-MODULE COMPOSITION (godyn \(-> godyn)'
  assert_success
  assert_output --partial "Heading with an arrow and parentheses."
}

function man_section_opens_heading_with_literal_arrow { # @test
  run "$BIN/section" "hyphen-heading(7)" "CROSS-MODULE COMPOSITION (godyn → godyn) LITERAL"
  assert_success
  assert_output --partial "literal UTF"
}

function man_section_stops_at_next_heading_after_hyphen_section { # @test
  run "$BIN/section" "hyphen-heading(7)" "THE GENERATE-COMMIT-EVALUATE WORKFLOW"
  assert_success
  refute_output --partial "arrow and parentheses"
}

# Every title man_toc lists must round-trip through man_section.
function man_toc_titles_round_trip_through_section { # @test
  local page title toc
  for page in "pivy-tool" "hyphen-heading(7)"; do
    toc=$("$BIN/toc" "$page")
    while IFS= read -r line; do
      [[ -z $line ]] && continue
      title="${line#\#\# }"
      title="${title#\# }"
      run "$BIN/section" "$page" "$title"
      if [[ $status -ne 0 ]]; then
        echo "round-trip failed: $page / $title" >&2
        return 1
      fi
    done <<<"$toc"
  done
}

function man_section_error_message_lists_both_sections_and_subsections { # @test
  run "$BIN/section" "pivy-tool" "Nonexistent Section"
  assert_failure
  # Top-level section appears
  assert_output --partial "OPERATIONS"
  # Subsection appears (indented under its parent)
  assert_output --partial "Informational"
}
