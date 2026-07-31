#! /usr/bin/env bats

# bats file_tags=grit

# grit.branch controls the HEAD / branch pointer: create, delete, soft, hard,
# checkout. The checkout cases here were migrated from the former grit.checkout
# tool (regression #307 for paths-as-string is preserved).

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

# --- create / delete -------------------------------------------------------

function grit_branch_create { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"create","name":"feature"}}'
  assert_success

  run git branch --list feature
  refute_output ""
}

function grit_branch_create_requires_name { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"create"}}'
  assert_output --partial "name is required"
}

function grit_branch_delete { # @test
  git branch feature

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"delete","name":"feature","force":true}}'
  assert_success

  run git branch --list feature
  assert_output ""
}

# main/master deletion is blocked for safety (preserved from branch-delete).
# The block keys off the branch NAME, not the current HEAD, so no master branch
# need actually exist — the guard fires before git runs.
function grit_branch_delete_main_blocked { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"delete","name":"master","force":true}}'
  echo "$output" | jq -e '.isError == true' || fail 'expected isError: '"$output"
  echo "$output" | jq -e '.content[0].text | test("blocked for safety")' || fail 'expected safety block: '"$output"
}

# --- soft / hard (pointer moves) -------------------------------------------

# soft moves HEAD back but keeps the working tree + index.
function grit_branch_soft_keeps_tree { # @test
  echo "b" >>file.txt
  git commit -aqm "second"

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"reset-soft","ref":"HEAD~1"}}'
  assert_success

  # HEAD moved back one commit...
  run git log --oneline
  assert_output --partial "initial"
  refute_output --partial "second"
  # ...but the change is still staged (soft, not hard).
  run git diff --cached --name-only
  assert_output "file.txt"
}

# hard moves HEAD and discards the working tree.
function grit_branch_hard_discards_tree { # @test
  git checkout -q -b work
  echo "b" >>file.txt
  git commit -aqm "second"

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"reset-hard","ref":"HEAD~1"}}'
  assert_success

  run cat file.txt
  assert_output "a"
}

function grit_branch_hard_requires_ref { # @test
  git checkout -q -b work

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"reset-hard"}}'
  assert_output --partial "ref is required"
}

# hard reset on main/master is blocked for safety (preserved from hard-reset).
function grit_branch_hard_main_blocked { # @test
  # Pin HEAD to master regardless of the repo's init.defaultBranch.
  git branch -m master

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"reset-hard","ref":"HEAD~1"}}'
  echo "$output" | jq -e '.isError == true' || fail 'expected isError: '"$output"
  echo "$output" | jq -e '.content[0].text | test("blocked for safety")' || fail 'expected safety block: '"$output"
}

# --- checkout (migrated from grit.checkout) --------------------------------

function grit_branch_checkout_restore_paths_array { # @test
  echo "modified" >file.txt

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"checkout","ref":"HEAD","paths":["file.txt"]}}'
  assert_success

  refute_output --partial "parse error"
  grep -qx "a" file.txt
}

# Regression for #307: `paths` passed as a string (not an array) must not emit a
# jq parse error, and must still restore the file.
function grit_branch_checkout_restore_paths_string { # @test
  echo "modified" >file.txt

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"checkout","ref":"HEAD","paths":"file.txt"}}'
  assert_success

  refute_output --partial "parse error"
  grep -qx "a" file.txt
}

function grit_branch_checkout_branch_switch { # @test
  git branch other

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"checkout","ref":"other"}}'
  assert_success

  [ "$(git rev-parse --abbrev-ref HEAD)" = "other" ]
}

function grit_branch_checkout_create_branch { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"checkout","ref":"newbranch","new_branch":true}}'
  assert_success

  [ "$(git rev-parse --abbrev-ref HEAD)" = "newbranch" ]
}

function grit_branch_requires_subcommand { # @test
  run_moxy_mcp "tools/call" '{"name":"grit.branch","arguments":{}}'
  assert_output --partial "subcommand"
}
