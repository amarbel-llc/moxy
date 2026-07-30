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

  printf 'a-original\n' >a.txt
  printf 'b-original\n' >b.txt
  git add a.txt b.txt
  git commit -m "initial"
}

teardown() {
  teardown_test_home
}

# Without paths, the whole working tree is stashed (regression guard).
function grit_stash_save_stashes_all { # @test
  printf 'a-modified\n' >a.txt
  printf 'b-modified\n' >b.txt

  local params='{"name":"grit.stash","arguments":{"subcommand":"save","message":"wip"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  run git diff --name-only
  assert_output ""
}

# With paths, only the listed pathspecs are stashed; others stay modified.
function grit_stash_save_paths_subset { # @test
  printf 'a-modified\n' >a.txt
  printf 'b-modified\n' >b.txt

  local params='{"name":"grit.stash","arguments":{"subcommand":"save","paths":["a.txt"]}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  # a.txt was stashed (reverted to committed); b.txt is untouched.
  run cat a.txt
  assert_output "a-original"

  run git diff --name-only
  assert_output "b.txt"
}

# apply restores a stashed change without removing it from the stash list.
function grit_stash_apply_restores_without_dropping { # @test
  printf 'a-modified\n' >a.txt
  git stash push -m wip

  local params='{"name":"grit.stash","arguments":{"subcommand":"apply"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  # Change is back in the working tree...
  run cat a.txt
  assert_output "a-modified"

  # ...and the stash entry still exists (apply, not pop).
  run git stash list
  refute_output ""
}

# drop removes a stash entry from the list.
function grit_stash_drop_removes_entry { # @test
  printf 'a-modified\n' >a.txt
  git stash push -m wip

  local params='{"name":"grit.stash","arguments":{"subcommand":"drop","stash_ref":"stash@{0}"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  run git stash list
  assert_output ""
}

# subcommand is required; omitting it is an error, not a silent no-op.
function grit_stash_requires_subcommand { # @test
  local params='{"name":"grit.stash","arguments":{}}'
  run_moxy_mcp "tools/call" "$params"
  assert_output --partial "subcommand"
}
