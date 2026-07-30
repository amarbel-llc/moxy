#! /usr/bin/env bats

# bats file_tags=grit

# grit.worktree: list and remove git worktrees.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home

  export XDG_CACHE_HOME="$HOME/.cache"

  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  git init -q
  git config user.email "test@test.com"
  git config user.name "Test"
  git config commit.gpgSign false

  echo "a" >file.txt
  git add file.txt
  git commit -q -m "initial"
}

teardown() {
  teardown_test_home
}

function grit_worktree_list_shows_main { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.worktree","arguments":{"subcommand":"list"}}'
  assert_success

  # Porcelain output names the main worktree's path.
  assert_output --partial "worktree "
  assert_output --partial "$HOME/repo"
}

function grit_worktree_remove { # @test
  git worktree add -q "$HOME/wt" -b wt-branch

  run_moxy_mcp "tools/call" \
    '{"name":"grit.worktree","arguments":{"subcommand":"remove","path":"'"$HOME"'/wt"}}'
  assert_success

  run git worktree list --porcelain
  refute_output --partial "$HOME/wt"
}

function grit_worktree_remove_requires_path { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.worktree","arguments":{"subcommand":"remove"}}'
  assert_output --partial "path is required"
}

function grit_worktree_requires_subcommand { # @test
  run_moxy_mcp "tools/call" '{"name":"grit.worktree","arguments":{}}'
  assert_output --partial "subcommand"
}
