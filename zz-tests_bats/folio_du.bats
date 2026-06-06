#! /usr/bin/env bats

# bats file_tags=folio

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

function folio_du_returns_json_summary { # @test
  mkdir -p "$HOME/project/dir"
  printf 'a%.0s' {1..100} >"$HOME/project/dir/file.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.du" \
    '{name: $n, arguments: {path: "dir"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  # Small output under the default cache-results = "threshold" policy
  # (#319): plain text block, no resource wrapper.
  local entry
  entry=$(echo "$output" | jq -r '.content[0].text')
  echo "$entry" | jq -e '.path == "dir"' || fail '.path == "dir" check failed: '"$entry"
  echo "$entry" | jq -e '.bytes >= 100' || fail '.bytes >= 100 check failed: '"$entry"
  echo "$entry" | jq -e '.human != null' || fail '.human != null check failed: '"$entry"
}

function folio_du_with_flags_returns_raw_text { # @test
  mkdir -p "$HOME/project/dir"
  echo "hi" >"$HOME/project/dir/a"
  echo "hi" >"$HOME/project/dir/b"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.du" \
    '{name: $n, arguments: {path: "dir", flags: "-ah"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  # Small output stays a plain text block; the script-emitted mime is
  # stripped below the cache threshold (#319).
  echo "$output" | jq -e '.content[0].type == "text"' || fail '.content[0].type == "text" check failed: '"$output"
  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  echo "$text" | grep -q "dir/a"
  echo "$text" | grep -q "dir/b"
}

function folio_du_works_outside_cwd { # @test
  # Native layer no longer restricts to CWD; dynamic-perms gating happens
  # at the Claude Code hook layer (not in bats).
  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"
  echo "data" >"$HOME/other/file.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.du" --arg p "$HOME/other" \
    '{name: $n, arguments: {path: $p}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local entry
  entry=$(echo "$output" | jq -r '.content[0].text')
  echo "$entry" | jq -e --arg p "$HOME/other" '.path == $p' || fail '.path == $p check failed: '"$entry"
  echo "$entry" | jq -e '.bytes > 0' || fail '.bytes > 0 check failed: '"$entry"
}
