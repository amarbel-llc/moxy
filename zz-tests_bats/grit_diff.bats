#! /usr/bin/env bats

# bats file_tags=grit

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home

  export XDG_CACHE_HOME="$HOME/.cache"

  # Use the real grit moxin from the source tree.
  # MOXIN_PATH inherited from justfile

  # Create an isolated git repo.
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  git init
  git config user.email "test@test.com"
  git config user.name "Test"

  echo "original" >file.txt
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
  if [[ $text_blocks_with_mime -ne 0 ]]; then
    echo "Found $text_blocks_with_mime text block(s) with mimeType set — violates MCP spec" >&2
    echo "Output: $output" >&2
    return 1
  fi
}

# Helper: assert resource blocks have a non-null resource field.
assert_resource_blocks_have_resource_field() {
  local malformed
  malformed=$(echo "$output" | jq '[.content // [] | .[] | select(.type == "resource" and .resource == null)] | length')
  if [[ $malformed -ne 0 ]]; then
    echo "Found $malformed resource block(s) missing the resource field" >&2
    echo "Output: $output" >&2
    return 1
  fi
}

function grit_diff_unstaged_changes { # @test
  echo "modified" >file.txt

  local params='{"name":"grit.stage","arguments":{"subcommand":"diff"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0' || fail '.content | length > 0 check failed: '"$output"
  assert_no_mimetype_on_text_blocks
  assert_resource_blocks_have_resource_field
}

function grit_diff_staged_changes { # @test
  echo "staged change" >file.txt
  git add file.txt

  local params='{"name":"grit.stage","arguments":{"subcommand":"diff","staged":true}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0' || fail '.content | length > 0 check failed: '"$output"
  assert_no_mimetype_on_text_blocks
  assert_resource_blocks_have_resource_field
}

function grit_diff_stat_only { # @test
  echo "stat change" >file.txt

  local params='{"name":"grit.stage","arguments":{"subcommand":"diff","stat_only":true}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0' || fail '.content | length > 0 check failed: '"$output"
  assert_no_mimetype_on_text_blocks
  assert_resource_blocks_have_resource_field
}

function grit_diff_no_changes { # @test
  local params='{"name":"grit.stage","arguments":{"subcommand":"diff"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  assert_no_mimetype_on_text_blocks
  assert_resource_blocks_have_resource_field
}

# Regression test for #337: a diff body larger than Linux's per-argument
# exec limit (MAX_ARG_STRLEN, 128 KiB) killed the wrapper because the diff
# was passed to jq as an argv argument ("Argument list too long").
function grit_diff_large_diff { # @test
  seq -f 'original line %.0f' 1 15000 >big.txt
  git add big.txt
  git commit -m "add big file"
  seq -f 'replaced line %.0f' 1 15000 >big.txt

  local params='{"name":"grit.stage","arguments":{"subcommand":"diff"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.isError != true' || fail 'diff returned isError: '"$output"
  echo "$output" | jq -e '.content | length > 0' || fail '.content | length > 0 check failed: '"$output"
  assert_no_mimetype_on_text_blocks
  assert_resource_blocks_have_resource_field
}

function grit_diff_staged_stat_only { # @test
  echo "both" >file.txt
  git add file.txt

  local params='{"name":"grit.stage","arguments":{"subcommand":"diff","staged":true,"stat_only":true}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content | length > 0' || fail '.content | length > 0 check failed: '"$output"
  assert_no_mimetype_on_text_blocks
  assert_resource_blocks_have_resource_field
}

# The folded diff preserves diff's historic always-cache: bin/diff stamps
# _meta."moxy/cache":"always", and grit.stage's dynamic policy honors it, so a
# non-empty diff comes back as a cached resource block (madder:// URI), not
# inline text. The _meta intent must be stripped before the client sees it.
function grit_diff_always_caches_via_meta_intent { # @test
  echo "modified" >file.txt

  local params='{"name":"grit.stage","arguments":{"subcommand":"diff"}}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content[0].type == "resource"' || fail "want cached resource: $output"
  echo "$output" | jq -e '.content[0].resource.uri | startswith("madder://blobs/")' || fail "want madder URI: $output"
  refute_output --partial "moxy/cache"
}
