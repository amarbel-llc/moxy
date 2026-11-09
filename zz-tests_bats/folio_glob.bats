#! /usr/bin/env bats

# bats file_tags=folio

# Regression tests for #514: brace patterns and `vendor/*` answered
# "No files found" with no error.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  mkdir -p "$HOME/project/src" "$HOME/project/vendor/pivy/src"
  : >"$HOME/project/src/a.go"
  : >"$HOME/project/src/b.rs"
  : >"$HOME/project/src/c.txt"
  : >"$HOME/project/vendor/pivy/src/x.c"
  : >"$HOME/project/vendor/top.txt"
  cd "$HOME/project"
}

teardown() {
  teardown_test_home
}

# Runs folio.glob and leaves the text of the first content block in $text.
glob_text() {
  local params
  params=$(jq -cn --arg p "$1" '{name: "folio.glob", arguments: {pattern: $p}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  text=$(echo "$output" | jq -r '.content[0].text')
}

function folio_glob_plain_pattern_matches { # @test
  glob_text '**/*.go'
  [[ $text == "src/a.go" ]] || fail "unexpected: $text"
}

function folio_glob_brace_pattern_matches_both_kinds { # @test
  glob_text '**/*.{go,rs}'
  echo "$text" | grep -qx "src/a.go" || fail "missing a.go: $text"
  echo "$text" | grep -qx "src/b.rs" || fail "missing b.rs: $text"
  ! echo "$text" | grep -q "c.txt" || fail "c.txt matched: $text"
}

function folio_glob_nested_and_prefix_braces_match { # @test
  glob_text 'src/{a,b}.{go,rs}'
  echo "$text" | grep -qx "src/a.go" || fail "missing a.go: $text"
  echo "$text" | grep -qx "src/b.rs" || fail "missing b.rs: $text"
  glob_text 'src/{a.{go,x},c.txt}'
  echo "$text" | grep -qx "src/a.go" || fail "missing a.go: $text"
  echo "$text" | grep -qx "src/c.txt" || fail "missing c.txt: $text"
}

function folio_glob_overlapping_braces_list_each_file_once { # @test
  glob_text 'src/{a,a}.go'
  [[ $text == "src/a.go" ]] || fail "unexpected: $text"
}

function folio_glob_vendor_star_returns_files_and_explains_directories { # @test
  # vendor/* holds the file top.txt and the directory pivy.
  glob_text 'vendor/*'
  [[ $text == "vendor/top.txt" ]] || fail "unexpected: $text"
  # Directories only: the empty answer names why.
  glob_text 'vendor/*/'
  echo "$text" | grep -q "directories matched; glob returns files only" || fail "no directory hint: $text"
  glob_text 'vendor/pivy/*'
  echo "$text" | grep -q "directories matched" || fail "no directory hint: $text"
}

function folio_glob_no_match_says_so_plainly { # @test
  glob_text '**/*.nope'
  [[ $text == "No files found" ]] || fail "unexpected: $text"
}

function folio_glob_unclosed_brace_is_an_error_naming_the_construct { # @test
  local params
  params=$(jq -cn '{name: "folio.glob", arguments: {pattern: "**/*.{go,rs"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  echo "$output" | jq -e '.isError == true' || fail "expected isError: $output"
  echo "$output" | jq -r '.content[0].text' | grep -q "unclosed '{'" || fail "error does not name the construct: $output"
}
