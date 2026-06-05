#! /usr/bin/env bats

# bats file_tags=grit

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home

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

checkout_text() {
  echo "$output" | jq -r '.content[0].text // .content[0].resource.text // empty'
}

function grit_checkout_restore_paths_array { # @test
  echo "modified" >file.txt

  run_moxy_mcp "tools/call" \
    '{"name":"grit.checkout","arguments":{"ref":"HEAD","paths":["file.txt"]}}'
  assert_success

  refute_output --partial "parse error"
  grep -qx "a" file.txt
}

# Regression for #307: `paths` passed as a string (not an array) — the same
# client mistake and bare-string forwarding as #303 — must not emit a jq parse
# error, and must still restore the file. Before the fix, the swallowed jq
# failure left `git checkout HEAD --` with no pathspec, so the file was never
# restored and the parse error leaked into the result.
function grit_checkout_restore_paths_string { # @test
  echo "modified" >file.txt

  run_moxy_mcp "tools/call" \
    '{"name":"grit.checkout","arguments":{"ref":"HEAD","paths":"file.txt"}}'
  assert_success

  refute_output --partial "parse error"
  grep -qx "a" file.txt
}

function grit_checkout_branch_switch { # @test
  git branch other

  run_moxy_mcp "tools/call" \
    '{"name":"grit.checkout","arguments":{"ref":"other"}}'
  assert_success

  [ "$(git rev-parse --abbrev-ref HEAD)" = "other" ]
}

function grit_checkout_create_branch { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.checkout","arguments":{"ref":"newbranch","create":true}}'
  assert_success

  [ "$(git rev-parse --abbrev-ref HEAD)" = "newbranch" ]
}
