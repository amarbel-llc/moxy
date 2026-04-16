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

function tools_hidden_when_progressive_disclosure_enabled { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
progressive-disclosure = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  # Child tools should not appear
  local tool_count
  tool_count=$(echo "$output" | jq '[.tools[] | select(.name == "srv.execute-command")] | length')
  [[ $tool_count -eq 0 ]]
}

function exec_mcp_tool_visible_when_progressive_disclosure_enabled { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
progressive-disclosure = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "exec-mcp")'
}

function moxy_tools_resource_template_appears { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/templates/list
  assert_success
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "moxy://tools/{server}")'
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "moxy://tools/{server}/{tool}")'
}

function moxy_tools_resource_lists_tools { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"moxy://tools/srv"}'
  assert_success
  echo "$output" | jq -r '.contents[0].text' | jq -e '.[0].name == "execute-command"'
}

function moxy_tools_resource_returns_single_tool_schema { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"moxy://tools/srv/execute-command"}'
  assert_success
  echo "$output" | jq -r '.contents[0].text' | jq -e '.name == "execute-command"'
  echo "$output" | jq -r '.contents[0].text' | jq -e '.inputSchema.properties.cmd'
}

function exec_tool_calls_hidden_tool { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
progressive-disclosure = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"exec-mcp","arguments":{"server":"srv","tool":"execute-command","arguments":{"cmd":"hello"}}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "executed: hello"'
}

function exec_tool_with_ephemeral_and_progressive_disclosure { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
ephemeral = true
progressive-disclosure = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"exec-mcp","arguments":{"server":"srv","tool":"execute-command","arguments":{"cmd":"eph-test"}}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "executed: eph-test"'
}

function global_progressive_disclosure_hides_all_server_tools { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
progressive-disclosure = true

[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  local tool_count
  tool_count=$(echo "$output" | jq '[.tools[] | select(.name == "srv.execute-command")] | length')
  [[ $tool_count -eq 0 ]]
  # exec-mcp and restart should still be present
  echo "$output" | jq -e '.tools[] | select(.name == "exec-mcp")'
}

function per_server_override_disables_progressive_disclosure { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
progressive-disclosure = true

[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
progressive-disclosure = false
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "srv.execute-command")'
}

function moxy_tools_resource_appears_in_resources_list { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/list
  assert_success
  echo "$output" | jq -e '.resources[] | select(.uri == "moxy://tools/srv")'
}
