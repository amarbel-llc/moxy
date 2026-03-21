#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
  FIXTURES_DIR="$(cd "$(dirname "$BATS_TEST_FILE")/test-fixtures" && pwd)"
}

teardown() {
  teardown_test_home
}

function tool_names_use_snob_case { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  # Hyphenated tool name should appear with underscores
  echo "$output" | jq -e '.tools[] | select(.name == "srv-execute_command")'
  # Original hyphenated form should NOT appear
  local hyphen_count
  hyphen_count=$(echo "$output" | jq '[.tools[] | select(.name == "srv-execute-command")] | length')
  [[ "$hyphen_count" -eq 0 ]]
}

function snob_case_tool_call_dispatches_correctly { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"srv-execute_command","arguments":{"cmd":"hello"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "executed: hello"'
}
