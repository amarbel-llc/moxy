#! /usr/bin/env bats

# bats file_tags=grit

# grit.stage controls the index/stage: add, unstage, rm, mv. The rm cases here
# were migrated from the former grit.rm tool (regression #288 for recursive is
# preserved).

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home

  export XDG_CACHE_HOME="$HOME/.cache"

  # Isolated git repo with a tracked file and a tracked subdir.
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  git init
  git config user.email "test@test.com"
  git config user.name "Test"

  echo "top" >file.txt
  mkdir -p subdir
  echo "nested" >subdir/nested.txt
  git add file.txt subdir/nested.txt
  git commit -m "initial"
}

teardown() {
  teardown_test_home
}

# --- add -------------------------------------------------------------------

function grit_stage_add_stages_file { # @test
  echo "newcontent" >new.txt

  run_moxy_mcp "tools/call" \
    '{"name":"grit.stage","arguments":{"subcommand":"add","paths":["new.txt"]}}'
  assert_success

  run git -C "$HOME/repo" status --porcelain new.txt
  assert_output "A  new.txt"
}

function grit_stage_add_requires_paths { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.stage","arguments":{"subcommand":"add"}}'
  assert_output --partial "paths is required"
}

# --- unstage (the index-only half of the old reset tool) -------------------

function grit_stage_unstage_paths { # @test
  echo "modified" >file.txt
  git add file.txt

  run_moxy_mcp "tools/call" \
    '{"name":"grit.stage","arguments":{"subcommand":"unstage","paths":["file.txt"]}}'
  assert_success

  # No longer staged, but the working-tree modification remains (unstage, not
  # a hard reset).
  run git -C "$HOME/repo" status --porcelain file.txt
  assert_output " M file.txt"
}

# --- rm (migrated from grit.rm) --------------------------------------------

function grit_stage_rm_single_file { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.stage","arguments":{"subcommand":"rm","paths":["file.txt"]}}'
  assert_success

  echo "$output" | jq -e '.isError != true' || fail '.isError != true check failed: '"$output"
  run git -C "$HOME/repo" status --porcelain file.txt
  assert_output "D  file.txt"
}

# Regression for #288: `recursive: true` must forward -r to `git rm`.
function grit_stage_rm_directory_recursive { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.stage","arguments":{"subcommand":"rm","paths":["subdir"],"recursive":true}}'
  assert_success

  echo "$output" | jq -e '.isError != true' || fail '.isError != true check failed: '"$output"
  run git -C "$HOME/repo" status --porcelain subdir/nested.txt
  assert_output "D  subdir/nested.txt"
}

# Without recursive, removing a directory must fail (guards against -r being
# silently always-on).
function grit_stage_rm_directory_without_recursive_fails { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.stage","arguments":{"subcommand":"rm","paths":["subdir"]}}'
  assert_success

  echo "$output" | jq -e '.isError == true' || fail '.isError == true check failed: '"$output"
  echo "$output" | jq -e '.content[0].text | test("not removing")' || fail '.content[0].text | test("not removing") check failed: '"$output"
}

# --- mv --------------------------------------------------------------------

function grit_stage_mv_renames_and_stages { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.stage","arguments":{"subcommand":"mv","source":"file.txt","destination":"renamed.txt"}}'
  assert_success

  run git -C "$HOME/repo" status --porcelain
  assert_output --partial "R  file.txt -> renamed.txt"
}

function grit_stage_mv_requires_source_and_destination { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.stage","arguments":{"subcommand":"mv","source":"file.txt"}}'
  assert_output --partial "source and destination are required"
}

function grit_stage_requires_subcommand { # @test
  run_moxy_mcp "tools/call" '{"name":"grit.stage","arguments":{}}'
  assert_output --partial "subcommand"
}
