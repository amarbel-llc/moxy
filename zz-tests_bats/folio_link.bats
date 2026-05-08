#! /usr/bin/env bats

# bats file_tags=folio

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function folio_link_creates_symlink_by_default { # @test
  mkdir -p "$HOME/project"
  echo "data" >"$HOME/project/orig.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.link" \
    '{name: $n, arguments: {source: "orig.txt", target: "alias.txt"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  [ -L "$HOME/project/alias.txt" ]
  [ "$(readlink "$HOME/project/alias.txt")" = "orig.txt" ]
}

function folio_link_creates_hardlink_when_symbolic_false { # @test
  mkdir -p "$HOME/project"
  echo "data" >"$HOME/project/orig.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.link" \
    '{name: $n, arguments: {source: "orig.txt", target: "hard.txt", symbolic: false}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  [ ! -L "$HOME/project/hard.txt" ]
  [ -f "$HOME/project/hard.txt" ]
  local orig_inode hard_inode
  orig_inode=$(stat -c '%i' "$HOME/project/orig.txt")
  hard_inode=$(stat -c '%i' "$HOME/project/hard.txt")
  [ "$orig_inode" = "$hard_inode" ]
}

function folio_link_force_replaces_existing_target { # @test
  mkdir -p "$HOME/project"
  echo "data" >"$HOME/project/orig.txt"
  echo "stale" >"$HOME/project/alias.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.link" \
    '{name: $n, arguments: {source: "orig.txt", target: "alias.txt", force: true}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  [ -L "$HOME/project/alias.txt" ]
  [ "$(readlink "$HOME/project/alias.txt")" = "orig.txt" ]
}

function folio_link_without_force_fails_on_existing_target { # @test
  mkdir -p "$HOME/project"
  echo "data" >"$HOME/project/orig.txt"
  echo "stale" >"$HOME/project/alias.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.link" \
    '{name: $n, arguments: {source: "orig.txt", target: "alias.txt"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "File exists"

  # Pre-existing file untouched.
  [ ! -L "$HOME/project/alias.txt" ]
  [ "$(cat "$HOME/project/alias.txt")" = "stale" ]
}

function folio_link_creates_parent_directory { # @test
  mkdir -p "$HOME/project"
  echo "data" >"$HOME/project/orig.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.link" \
    '{name: $n, arguments: {source: "orig.txt", target: "subdir/alias.txt"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  [ -d "$HOME/project/subdir" ]
  [ -L "$HOME/project/subdir/alias.txt" ]
  [ "$(readlink "$HOME/project/subdir/alias.txt")" = "orig.txt" ]
}
