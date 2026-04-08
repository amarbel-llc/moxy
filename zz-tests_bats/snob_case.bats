#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"
}

teardown() {
  teardown_test_home
}

function tool_names_use_dot_separator { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  # Tool name should be server.original-name (dot separator, name preserved)
  echo "$output" | jq -e '.tools[] | select(.name == "srv.execute-command")'
}

function dot_separator_tool_call_dispatches_correctly { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"srv.execute-command","arguments":{"cmd":"hello"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "executed: hello"'
}
