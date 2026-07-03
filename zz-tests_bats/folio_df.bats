#! /usr/bin/env bats

# bats file_tags=folio

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

function folio_df_returns_json_fields { # @test
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.df" '{name: $n, arguments: {}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local entry
  entry=$(echo "$output" | jq -r '.content[0].text')
  echo "$entry" | jq -e 'has("filesystem") and has("size_bytes") and has("used_bytes") and has("avail_bytes") and has("use_percent") and has("mount_point") and has("human")' ||
    fail 'missing df fields: '"$entry"
  echo "$entry" | jq -e '.size_bytes | type == "number"' || fail '.size_bytes not a number: '"$entry"
  echo "$entry" | jq -e '.avail_bytes | type == "number"' || fail '.avail_bytes not a number: '"$entry"
  echo "$entry" | jq -e '.use_percent | type == "number"' || fail '.use_percent not a number: '"$entry"
}

function folio_df_explicit_path { # @test
  mkdir -p "$HOME/other"
  cd "$HOME"

  local params
  params=$(jq -cn --arg n "folio.df" --arg p "$HOME/other" \
    '{name: $n, arguments: {path: $p}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local entry
  entry=$(echo "$output" | jq -r '.content[0].text')
  echo "$entry" | jq -e '.size_bytes > 0' || fail '.size_bytes > 0 check failed: '"$entry"
  echo "$entry" | jq -e '.mount_point != ""' || fail '.mount_point non-empty check failed: '"$entry"
}
