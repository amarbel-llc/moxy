#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function folio_du_returns_json_summary { # @test
  mkdir -p "$HOME/project/dir"
  printf 'a%.0s' {1..100} > "$HOME/project/dir/file.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.du" \
    '{name: $n, arguments: {path: "dir"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local entry
  entry=$(echo "$output" | jq -r '.content[0].resource.text')
  echo "$entry" | jq -e '.path == "dir"'
  echo "$entry" | jq -e '.bytes >= 100'
  echo "$entry" | jq -e '.human != null'
}

function folio_du_with_flags_returns_raw_text { # @test
  mkdir -p "$HOME/project/dir"
  echo "hi" > "$HOME/project/dir/a"
  echo "hi" > "$HOME/project/dir/b"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.du" \
    '{name: $n, arguments: {path: "dir", flags: "-ah"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.content[0].resource.mimeType == "text/plain"'
  local text
  text=$(echo "$output" | jq -r '.content[0].resource.text')
  echo "$text" | grep -q "dir/a"
  echo "$text" | grep -q "dir/b"
}

function folio_du_rejects_path_outside_cwd { # @test
  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.du" --arg p "$HOME/other" \
    '{name: $n, arguments: {path: $p}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  assert_output --partial "outside CWD"
}

function folio_external_du_works_outside_cwd { # @test
  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"
  echo "data" > "$HOME/other/file.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio-external.du" --arg p "$HOME/other" \
    '{name: $n, arguments: {path: $p}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local entry
  entry=$(echo "$output" | jq -r '.content[0].resource.text')
  echo "$entry" | jq -e --arg p "$HOME/other" '.path == $p'
  echo "$entry" | jq -e '.bytes > 0'
}
