#! /usr/bin/env bats

# bats file_tags=grit

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home

  export XDG_CACHE_HOME="$HOME/.cache"

  # Use the real grit moxin from the source tree.
  # MOXIN_PATH inherited from justfile

  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  git init
  git config user.email "test@test.com"
  git config user.name "Test"

  printf 'a-original\n' > a.txt
  printf 'b-original\n' > b.txt
  git add a.txt b.txt
  git commit -m "initial"
}

teardown() {
  teardown_test_home
}

# Without paths, the whole working tree is stashed (regression guard).
function grit_stash_save_stashes_all { # @test
  printf 'a-modified\n' > a.txt
  printf 'b-modified\n' > b.txt

  local params='{"name":"grit.stash-save","arguments":{"message":"wip"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  run git diff --name-only
  assert_output ""
}

# With paths, only the listed pathspecs are stashed; others stay modified.
function grit_stash_save_paths_subset { # @test
  printf 'a-modified\n' > a.txt
  printf 'b-modified\n' > b.txt

  local params='{"name":"grit.stash-save","arguments":{"paths":["a.txt"]}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  # a.txt was stashed (reverted to committed); b.txt is untouched.
  run cat a.txt
  assert_output "a-original"

  run git diff --name-only
  assert_output "b.txt"
}
