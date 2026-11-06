#! /usr/bin/env bats

# bats file_tags=inventory

# moxy#116: the shipped moxy, run with nothing but its bundled moxins (no
# MOXIN_PATH, a bare HOME), serves exactly the tools in
# test-fixtures/bundled-moxin-tools.txt. A moxin that silently stops loading
# (a parser change rejecting TOML that used to load, #71) shows up here as
# missing tools, or as a <moxin>.status tool for one that failed. Adding or
# removing a bundled tool means updating the list on purpose.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  unset MOXIN_PATH
  mkdir -p "$HOME/project"
  cd "$HOME/project"
}

teardown() {
  teardown_test_home
}

function the_bundled_moxins_serve_exactly_the_expected_tools { # @test
  run_moxy_mcp "tools/list"
  assert_success
  # Meta tools (restart, batch, async...) have no dot; every dotted name is a
  # moxin's (or a failed server's status) tool.
  served=$(jq -r '.tools[].name | select(contains("."))' <<<"$output" | LC_ALL=C sort)
  expected=$(LC_ALL=C sort "$BATS_TEST_DIRNAME/test-fixtures/bundled-moxin-tools.txt")
  [ "$served" = "$expected" ] || {
    echo "bundled tool inventory changed (- expected, + served):" >&2
    diff <(echo "$expected") <(echo "$served") >&2 || true
    false
  }
}
