#! /usr/bin/env bats

# bats file_tags=grit

# grit.remote: fetch, pull, push, push-stack. Tests use a local bare repo as
# the remote so no network is involved.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home

  export XDG_CACHE_HOME="$HOME/.cache"

  # Bare "remote".
  git init -q --bare "$HOME/remote.git"

  # Working clone with an initial commit pushed to the remote.
  git clone -q "$HOME/remote.git" "$HOME/repo"
  cd "$HOME/repo"
  git config user.email "test@test.com"
  git config user.name "Test"
  git config commit.gpgSign false
  echo "a" >a.txt
  git add a.txt
  git commit -q -m "initial"
  git push -q origin HEAD:refs/heads/main 2>/dev/null || git push -q origin HEAD
  git branch --set-upstream-to=origin/main 2>/dev/null || true
}

teardown() {
  teardown_test_home
}

# --- fetch -----------------------------------------------------------------

function grit_remote_fetch { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.remote","arguments":{"subcommand":"fetch","remote":"origin"}}'
  assert_success
  echo "$output" | jq -e '.isError != true' || fail 'expected no error: '"$output"
}

# --- push ------------------------------------------------------------------

function grit_remote_push_new_commit { # @test
  echo "b" >>a.txt
  git commit -aqm "second"

  run_moxy_mcp "tools/call" \
    '{"name":"grit.remote","arguments":{"subcommand":"push","remote":"origin","branch":"HEAD"}}'
  assert_success
  echo "$output" | jq -e '.isError != true' || fail 'expected no error: '"$output"

  # The remote now has the second commit.
  run git --git-dir="$HOME/remote.git" log --oneline
  assert_output --partial "second"
}

# Force-push to main/master is blocked for safety (preserved from push).
function grit_remote_force_push_main_blocked { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.remote","arguments":{"subcommand":"push","remote":"origin","branch":"main","force_with_lease":true}}'
  echo "$output" | jq -e '.isError == true' || fail 'expected isError: '"$output"
  echo "$output" | jq -e '.content[0].text | test("blocked for safety")' || fail 'expected safety block: '"$output"
}

# --- pull ------------------------------------------------------------------

function grit_remote_pull { # @test
  # Push a commit from a second clone, then pull it here.
  git clone -q "$HOME/remote.git" "$HOME/repo2"
  (
    cd "$HOME/repo2"
    git config user.email "t2@test.com"
    git config user.name "Test2"
    git config commit.gpgSign false
    echo "c" >>a.txt
    git commit -aqm "from repo2"
    git push -q origin HEAD
  )

  run_moxy_mcp "tools/call" \
    '{"name":"grit.remote","arguments":{"subcommand":"pull","remote":"origin"}}'
  assert_success
  echo "$output" | jq -e '.isError != true' || fail 'expected no error: '"$output"

  run git log --oneline
  assert_output --partial "from repo2"
}

# --- push-stack (execs the bun binary) -------------------------------------

# push-stack returns its JSON {phase, results} report; a main/master branch in
# the chain is rejected up front by the TS guard.
function grit_remote_push_stack_rejects_main { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"grit.remote","arguments":{"subcommand":"push-stack","branches":["main"],"remote":"origin"}}'
  # The TS throws on main/master before any push; surfaced as an error result.
  echo "$output" | jq -e '.content[0].text | test("blocked for safety")' || fail 'expected main/master block: '"$output"
}

# push-stack of a real feature branch reports a JSON push result.
function grit_remote_push_stack_pushes_branch { # @test
  git checkout -q -b feature
  echo "d" >>a.txt
  git commit -aqm "feature commit"

  run_moxy_mcp "tools/call" \
    '{"name":"grit.remote","arguments":{"subcommand":"push-stack","branches":["feature"],"remote":"origin"}}'
  assert_success

  # push-stack writes its JSON report to stdout; git's push progress lands on
  # stderr and moxy merges both into the result text, so the text is the JSON
  # object followed by git's "To <remote> / * [new branch]" lines. Assert on
  # the JSON content (branch + ok status) via a partial match rather than
  # fromjson-ing the whole merged blob.
  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  echo "$text" | grep -q '"branch": "feature"' ||
    fail 'expected push-stack JSON naming feature: '"$output"
  echo "$text" | grep -q '"status": "ok"' ||
    fail 'expected push-stack ok status: '"$output"

  # And the branch really landed on the remote.
  run git --git-dir="$HOME/remote.git" rev-parse --verify feature
  assert_success
}

function grit_remote_requires_subcommand { # @test
  run_moxy_mcp "tools/call" '{"name":"grit.remote","arguments":{}}'
  assert_output --partial "subcommand"
}
