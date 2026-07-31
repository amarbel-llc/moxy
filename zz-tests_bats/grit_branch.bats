#! /usr/bin/env bats

# bats file_tags=grit

# grit.branch owns the branch as an object: pointer (create/delete/reset-soft/
# reset-hard/checkout), history (rebase/restack/cherry-pick/revert), and inspect
# (log/rev-parse/diff). The checkout cases were migrated from the former
# grit.checkout tool (regression #307 for paths-as-string is preserved). The
# intricate rebase/restack scenarios live in grit_rebase.bats / grit_restack.bats
# (direct bin/ callers); here we cover the grit.branch subcommand surface.

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

# --- inspect: log / rev-parse / diff ---------------------------------------

branch_text() {
  echo "$output" | jq -r '.content[0].text // .content[0].resource.text // empty'
}

function grit_branch_log_shows_history { # @test
  run_moxy_mcp_v1 "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"log","oneline":true}}'
  assert_success
  branch_text | grep -q "initial"
}

function grit_branch_rev_parse_resolves_head { # @test
  run_moxy_mcp_v1 "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"rev-parse","ref":"HEAD"}}'
  assert_success
  # A full 40-char SHA.
  branch_text | grep -Eq '^[0-9a-f]{40}$'
}

function grit_branch_rev_parse_requires_ref { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"rev-parse"}}'
  assert_output --partial "ref is required"
}

# The ref-form diff (grit.branch) diffs against a ref; it has no `staged` flag.
function grit_branch_diff_against_ref { # @test
  echo "b" >>file.txt
  git commit -aqm "second"

  run_moxy_mcp_v1 "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"diff","ref":"HEAD~1"}}'
  assert_success
  # Non-empty diff → cached resource via bin/diff's always-cache _meta intent.
  echo "$output" | jq -e '.content[0].type == "resource"' || fail "want cached resource: $output"
  refute_output --partial "moxy/cache"
}

# --- history: cherry-pick / revert / rebase --------------------------------

function grit_branch_cherry_pick { # @test
  # A commit on another branch, cherry-picked onto a fresh branch.
  git checkout -q -b feature
  echo "picked" >picked.txt
  git add picked.txt
  git commit -q -m "add picked"
  local sha
  sha=$(git rev-parse HEAD)
  git checkout -q -
  git checkout -q -b target

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"cherry-pick","commits":["'"$sha"'"]}}'
  assert_success
  [ -f picked.txt ]
}

function grit_branch_cherry_pick_requires_commits { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"cherry-pick"}}'
  assert_output --partial "commits is required"
}

function grit_branch_revert_appends_undo_commit { # @test
  echo "b" >>file.txt
  git commit -aqm "second"
  local sha
  sha=$(git rev-parse HEAD)

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"revert","commits":["'"$sha"'"]}}'
  assert_success
  # revert appends a new commit (history grows, not rewinds).
  run git log --oneline
  assert_output --partial "Revert"
}

# rebase smoke through grit.branch (deep scenarios are in grit_rebase.bats).
function grit_branch_rebase_onto { # @test
  git checkout -q -b topic
  echo "t" >t.txt
  git add t.txt
  git commit -q -m "topic commit"

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"rebase","upstream":"master"}}' ||
    run_moxy_mcp "tools/call" \
      '{"name":"grit.branch","arguments":{"subcommand":"rebase","upstream":"main"}}'
  assert_success
}

# rebase on main/master is blocked (via the bin/rebase helper's guard).
function grit_branch_rebase_main_blocked { # @test
  git branch -m master

  run_moxy_mcp "tools/call" \
    '{"name":"grit.branch","arguments":{"subcommand":"rebase","upstream":"HEAD","rebase_branch":"master"}}'
  echo "$output" | jq -e '.isError == true' || fail 'expected isError: '"$output"
  echo "$output" | jq -e '.content[0].text | test("blocked for safety")' || fail 'expected safety block: '"$output"
}
