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

function ephemeral_server_tools_appear_in_list { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
ephemeral = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "srv.execute-command")'
}

function ephemeral_server_tool_call_succeeds { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
ephemeral = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"srv.execute-command","arguments":{"cmd":"ephemeral-test"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "executed: ephemeral-test"'
}

function ephemeral_server_stderr_shows_probed { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
ephemeral = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp_with_stderr tools/list
  assert_success
  echo "$MOXY_STDERR" | grep -q "ephemeral"
  echo "$MOXY_STDERR" | grep -q "probed"
}

function global_ephemeral_applies_to_all_servers { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
ephemeral = true

[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp_with_stderr tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "srv.execute-command")'
  echo "$MOXY_STDERR" | grep -q "ephemeral"
}

function per_server_ephemeral_overrides_global { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
ephemeral = true

[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
ephemeral = false
EOF

  cd "$HOME/repo"
  run_moxy_mcp_with_stderr tools/list
  assert_success
  # Server should be connected normally, not probed as ephemeral
  echo "$MOXY_STDERR" | grep -q "connected to srv"
}

function ephemeral_and_persistent_coexist { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "persistent"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]

[[servers]]
name = "ephemeral-srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
ephemeral = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "persistent.execute-command")'
  echo "$output" | jq -e '.tools[] | select(.name == "ephemeral-srv.execute-command")'
}

function ephemeral_restart_reprobes { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
ephemeral = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"restart","arguments":{"server":"srv"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text | test("restarted successfully")'
}
