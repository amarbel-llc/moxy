#! /usr/bin/env bats

# bats file_tags=grit

# The read-only grit tools (moxy#510): grit.status, grit.diff, grit.log,
# grit.rev-parse, grit.tag-list and grit.worktree-list. Each runs one git read
# verb, is always-allow, and has no way to reach a write verb, so a profile
# that must stay read-only can keep them while dropping the mixed tools.

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

  echo "a" >tracked.txt
  git add tracked.txt
  git commit -q -m "initial"
  git tag v1.0
}

teardown() {
  teardown_test_home
}

# The text of a result, whether returned inline or as a cached blob resource.
result_text() {
  echo "$output" | jq -r '.content[0].text // .content[0].resource.text // empty'
}

readonly_tools=(grit.status grit.diff grit.log grit.rev-parse grit.tag-list grit.worktree-list)

function grit_readonly_tools_are_listed_read_only { # @test
  # V1 carries tool annotations; V0 drops them.
  run_moxy_mcp_v1 "tools/list" '{}'
  assert_success

  local t
  for t in "${readonly_tools[@]}"; do
    echo "$output" | jq -e --arg t "$t" '.tools[] | select(.name == $t)' >/dev/null ||
      fail "$t is not listed"
    # read-only by annotation, and no subcommand or repo_path to steer it
    echo "$output" | jq -e --arg t "$t" \
      '.tools[] | select(.name == $t) | .annotations.readOnlyHint == true' >/dev/null ||
      fail "$t is not annotated read-only"
    echo "$output" | jq -e --arg t "$t" \
      '.tools[] | select(.name == $t) | (.inputSchema.properties // {}) | has("subcommand") or has("repo_path") | not' >/dev/null ||
      fail "$t exposes subcommand or repo_path"
  done
}

function grit_status_reports_branch_and_untracked { # @test
  echo "b" >untracked.txt

  run_moxy_mcp "tools/call" '{"name":"grit.status","arguments":{}}'
  assert_success
  local t
  t=$(result_text)
  echo "$t" | grep -q '## '
  echo "$t" | grep -q '?? untracked.txt'
}

function grit_status_untracked_no { # @test
  echo "b" >untracked.txt

  run_moxy_mcp "tools/call" '{"name":"grit.status","arguments":{"untracked":"no"}}'
  assert_success
  local t
  t=$(result_text)
  ! echo "$t" | grep -q 'untracked.txt'
}

# A plain `git status` rewrites .git/index to refresh stat data; the
# read-only tool takes no optional locks, so the index stays byte-identical.
function grit_status_does_not_rewrite_the_index { # @test
  touch -d '2001-01-01' tracked.txt
  git update-index --really-refresh >/dev/null
  touch tracked.txt # stat-dirty again, contents unchanged
  cp .git/index "$BATS_TEST_TMPDIR/index.before"

  run_moxy_mcp "tools/call" '{"name":"grit.status","arguments":{}}'
  assert_success

  cmp .git/index "$BATS_TEST_TMPDIR/index.before"
}

function grit_diff_shows_unstaged_then_staged { # @test
  echo "changed" >tracked.txt

  run_moxy_mcp "tools/call" '{"name":"grit.diff","arguments":{}}'
  assert_success
  result_text | grep -q '^+changed'

  git add tracked.txt
  run_moxy_mcp "tools/call" '{"name":"grit.diff","arguments":{}}'
  assert_success
  [ -z "$(result_text)" ]

  run_moxy_mcp "tools/call" '{"name":"grit.diff","arguments":{"staged":true}}'
  assert_success
  result_text | grep -q '^+changed'
}

function grit_diff_against_a_ref_and_stat_only { # @test
  echo "second" >>tracked.txt
  git commit -q -am "second"

  run_moxy_mcp "tools/call" '{"name":"grit.diff","arguments":{"ref":"HEAD~1","stat_only":true}}'
  assert_success
  result_text | grep -q 'tracked.txt'
  result_text | grep -q '1 file changed'
}

# `git diff --output=FILE` writes a file; a ref that starts with "-" must be
# refused before git sees it.
function grit_diff_refuses_an_option_for_a_ref { # @test
  run_moxy_mcp "tools/call" '{"name":"grit.diff","arguments":{"ref":"--output=pwned.txt"}}'
  [ ! -e pwned.txt ]
  assert_output --partial "must not start with"
}

function grit_diff_ignores_a_configured_external_driver { # @test
  printf '#!/bin/sh\ntouch "%s/driver-ran"\n' "$HOME" >"$HOME/driver.sh"
  chmod +x "$HOME/driver.sh"
  git config diff.external "$HOME/driver.sh"
  echo "changed" >tracked.txt

  run_moxy_mcp "tools/call" '{"name":"grit.diff","arguments":{}}'
  assert_success
  [ ! -e "$HOME/driver-ran" ]
  result_text | grep -q '^+changed'
}

function grit_log_oneline_and_max_count { # @test
  echo "second" >>tracked.txt
  git commit -q -am "second commit"

  run_moxy_mcp "tools/call" '{"name":"grit.log","arguments":{"oneline":true,"max_count":1}}'
  assert_success
  local t
  t=$(result_text)
  echo "$t" | grep -q 'second commit'
  ! echo "$t" | grep -q 'initial'
}

function grit_log_refuses_an_option_for_a_ref { # @test
  run_moxy_mcp "tools/call" '{"name":"grit.log","arguments":{"ref":"--output=pwned.txt"}}'
  [ ! -e pwned.txt ]
  assert_output --partial "must not start with"
}

function grit_log_refuses_a_non_numeric_max_count { # @test
  run_moxy_mcp "tools/call" '{"name":"grit.log","arguments":{"max_count":"--output=pwned.txt"}}'
  [ ! -e pwned.txt ]
}

function grit_rev_parse_resolves_head { # @test
  run_moxy_mcp "tools/call" '{"name":"grit.rev-parse","arguments":{"ref":"HEAD"}}'
  assert_success
  [ "$(result_text)" = "$(git rev-parse HEAD)" ]
}

function grit_rev_parse_refuses_an_option_for_a_ref { # @test
  run_moxy_mcp "tools/call" '{"name":"grit.rev-parse","arguments":{"ref":"--git-dir"}}'
  assert_output --partial "must not start with"
}

function grit_tag_list_lists_and_filters { # @test
  git tag v2.0
  git tag other

  run_moxy_mcp "tools/call" '{"name":"grit.tag-list","arguments":{"pattern":"v*"}}'
  assert_success
  local t
  t=$(result_text)
  echo "$t" | grep -q 'v1.0'
  echo "$t" | grep -q 'v2.0'
  ! echo "$t" | grep -q 'other'
}

function grit_tag_list_max_count { # @test
  git tag v2.0
  git tag v3.0

  run_moxy_mcp "tools/call" '{"name":"grit.tag-list","arguments":{"max_count":2}}'
  assert_success
  [ "$(result_text | grep -c .)" -eq 2 ]
}

function grit_tag_list_refuses_an_option_pattern { # @test
  run_moxy_mcp "tools/call" '{"name":"grit.tag-list","arguments":{"pattern":"-d"}}'
  assert_output --partial "must not start with"
  [ -n "$(git tag --list v1.0)" ]
}

function grit_worktree_list_shows_main { # @test
  run_moxy_mcp "tools/call" '{"name":"grit.worktree-list","arguments":{}}'
  assert_success
  assert_output --partial "worktree "
  assert_output --partial "$HOME/repo"
}
