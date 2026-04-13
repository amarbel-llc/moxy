#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output

  export XDG_CACHE_HOME="$HOME/.cache"

  # Use the real grit moxin from the source tree.
  # MOXIN_PATH inherited from justfile

  # Create an isolated git repo.
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  git init
  git config user.email "test@test.com"
  git config user.name "Test"

  echo "original" > file.txt
  git add file.txt
  git commit -m "initial"
}

teardown() {
  teardown_test_home
}

# Helper: assert no text content blocks have mimeType set (MCP spec violation).
assert_no_mimetype_on_text_blocks() {
  local text_blocks_with_mime
  text_blocks_with_mime=$(echo "$output" | jq '[.content // [] | .[] | select(.type == "text" and .mimeType != null and .mimeType != "")] | length')
  if [[ "$text_blocks_with_mime" -ne 0 ]]; then
    echo "Found $text_blocks_with_mime text block(s) with mimeType set — violates MCP spec" >&2
    echo "Output: $output" >&2
    return 1
  fi
}

# Helper: assert resource blocks have a non-null resource field.
assert_resource_blocks_have_resource_field() {
  local malformed
  malformed=$(echo "$output" | jq '[.content // [] | .[] | select(.type == "resource" and .resource == null)] | length')
  if [[ "$malformed" -ne 0 ]]; then
    echo "Found $malformed resource block(s) missing the resource field" >&2
    echo "Output: $output" >&2
    return 1
  fi
}

function grit_diff_unstaged_changes { # @test
  echo "modified" > file.txt

  local params='{"name":"grit.diff"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0'
  assert_no_mimetype_on_text_blocks
  assert_resource_blocks_have_resource_field
}

function grit_diff_staged_changes { # @test
  echo "staged change" > file.txt
  git add file.txt

  local params='{"name":"grit.diff","arguments":{"staged":true}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0'
  assert_no_mimetype_on_text_blocks
  assert_resource_blocks_have_resource_field
}

function grit_diff_stat_only { # @test
  echo "stat change" > file.txt

  local params='{"name":"grit.diff","arguments":{"stat_only":true}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0'
  assert_no_mimetype_on_text_blocks
  assert_resource_blocks_have_resource_field
}

function grit_diff_no_changes { # @test
  local params='{"name":"grit.diff"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  assert_no_mimetype_on_text_blocks
  assert_resource_blocks_have_resource_field
}

function grit_diff_staged_stat_only { # @test
  echo "both" > file.txt
  git add file.txt

  local params='{"name":"grit.diff","arguments":{"staged":true,"stat_only":true}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0'
  assert_no_mimetype_on_text_blocks
  assert_resource_blocks_have_resource_field
}
