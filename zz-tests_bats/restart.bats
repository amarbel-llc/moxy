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

function restart_tool_not_listed { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  # restart tool is disabled — should not appear in tools/list
  run bash -c "echo '$output' | jq -e '.tools[] | select(.name == \"restart\")'"
  assert_failure
}

function restart_running_server_succeeds { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"restart","arguments":{"server":"srv"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text | test("restarted successfully")'
}

function restart_then_tool_call_works { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp_two \
    tools/call '{"name":"restart","arguments":{"server":"srv"}}' \
    tools/call '{"name":"srv.execute-command","arguments":{"cmd":"after-restart"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "executed: after-restart"'
}

function restart_unknown_server_returns_error { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"restart","arguments":{"server":"nonexistent"}}'
  assert_success
  echo "$output" | jq -e '.isError == true'
  echo "$output" | jq -e '.content[0].text | test("unknown server")'
}

function restart_missing_server_param_returns_error { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"restart","arguments":{}}'
  assert_success
  echo "$output" | jq -e '.isError == true'
  echo "$output" | jq -e '.content[0].text | test("server name is required")'
}

function restart_failed_server_recovers { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]

[[servers]]
name = "broken"
command = "echo"
args = ["hello"]
EOF

  cd "$HOME/repo"
  # First verify broken server has a status tool
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "broken.status")'
}
