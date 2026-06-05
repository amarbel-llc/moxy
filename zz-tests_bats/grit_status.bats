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

  echo "a" >tracked.txt
  git add tracked.txt
  git commit -q -m "initial"
}

teardown() {
  teardown_test_home
}

status_text() {
  echo "$output" | jq -r '.content[0].text // .content[0].resource.text // empty'
}

# Regression for #303: a plain status call must not emit a jq parse error.
function grit_status_plain_no_jq_parse_error { # @test
  echo "b" >untracked.txt

  run_moxy_mcp "tools/call" '{"name":"grit.status","arguments":{}}'
  assert_success

  local t
  t=$(status_text)
  echo "$t" | grep -q '## '
  echo "$t" | grep -q '?? untracked.txt'
  refute_output --partial "parse error"
}

function grit_status_with_paths_filter { # @test
  echo "b" >untracked.txt
  echo "c" >other.txt

  run_moxy_mcp "tools/call" \
    '{"name":"grit.status","arguments":{"paths":["untracked.txt"]}}'
  assert_success

  local t
  t=$(status_text)
  echo "$t" | grep -q '?? untracked.txt'
  refute_output --partial "parse error"
}

# Regression for #303: `paths` passed as a string (not an array) — a common
# client mistake, and what moxy forwards as a bare unquoted value — must not
# produce a jq parse error. The value is treated as a single path filter.
# A number-led name reproduces the reporter's "Invalid numeric literal" error.
function grit_status_paths_as_string_no_jq_error { # @test
  echo "b" >2024-report.md
  echo "c" >other.txt

  run_moxy_mcp "tools/call" \
    '{"name":"grit.status","arguments":{"paths":"2024-report.md"}}'
  assert_success

  local t
  t=$(status_text)
  refute_output --partial "parse error"
  echo "$t" | grep -q '?? 2024-report.md'
  # The string is treated as a single path filter, so other.txt is excluded.
  ! echo "$t" | grep -q "other.txt"
}

function grit_status_untracked_no_hides_untracked { # @test
  echo "b" >untracked.txt

  run_moxy_mcp "tools/call" \
    '{"name":"grit.status","arguments":{"untracked":"no"}}'
  assert_success

  local t
  t=$(status_text)
  ! echo "$t" | grep -q '?? untracked.txt'
  refute_output --partial "parse error"
}

# The reporter stressed "a working directory that is a git worktree". Run
# status from a linked worktree to exercise that path.
function grit_status_in_linked_worktree { # @test
  git worktree add -q "$HOME/wt" -b feature
  cd "$HOME/wt"
  echo "d" >wt-file.txt

  run_moxy_mcp "tools/call" '{"name":"grit.status","arguments":{}}'
  assert_success

  local t
  t=$(status_text)
  echo "$t" | grep -q '## '
  echo "$t" | grep -q '?? wt-file.txt'
  refute_output --partial "parse error"
}
