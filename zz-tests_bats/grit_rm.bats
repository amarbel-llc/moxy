#! /usr/bin/env bats

# bats file_tags=grit

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home

  export XDG_CACHE_HOME="$HOME/.cache"

  # Use the real grit moxin from the source tree.
  # MOXIN_PATH inherited from justfile

  # Create an isolated git repo with a tracked file and a tracked subdir.
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  git init
  git config user.email "test@test.com"
  git config user.name "Test"

  echo "top" > file.txt
  mkdir -p subdir
  echo "nested" > subdir/nested.txt
  git add file.txt subdir/nested.txt
  git commit -m "initial"
}

teardown() {
  teardown_test_home
}

function grit_rm_single_file { # @test
  local params='{"name":"grit.rm","arguments":{"paths":["file.txt"]}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.isError != true' || fail '.isError != true check failed: '"$output"
  # git should have staged the deletion.
  run git -C "$HOME/repo" status --porcelain file.txt
  assert_output "D  file.txt"
}

# Regression test for #288: `recursive: true` must forward -r to `git rm` so
# directory removals succeed instead of failing with "not removing ...
# recursively without -r".
function grit_rm_directory_recursive { # @test
  local params='{"name":"grit.rm","arguments":{"paths":["subdir"],"recursive":true}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.isError != true' || fail '.isError != true check failed: '"$output"
  run git -C "$HOME/repo" status --porcelain subdir/nested.txt
  assert_output "D  subdir/nested.txt"
}

# Without recursive, removing a directory must fail (git refuses without -r).
# This guards against the flag being silently always-on.
function grit_rm_directory_without_recursive_fails { # @test
  local params='{"name":"grit.rm","arguments":{"paths":["subdir"]}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.isError == true' || fail '.isError == true check failed: '"$output"
  echo "$output" | jq -e '.content[0].text | test("not removing")' || fail '.content[0].text | test("not removing") check failed: '"$output"
}
