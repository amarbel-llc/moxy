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

function synthetic_resource_read_tool_appears_in_tools_list { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "res-resource_read")'
}

function synthetic_resource_templates_tool_appears_in_tools_list { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "res-resource_templates")'
}

function synthetic_resource_read_tool_reads_resource { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"res-resource_read","arguments":{"uri":"test://items"}}'
  assert_success
  echo "$output" | jq -r '.content[0].text' | jq -e '.[0].text == "[1,2,3,4,5,6,7,8,9,10]"'
}

function synthetic_resource_templates_tool_returns_templates { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"res-resource_templates","arguments":{}}'
  assert_success
  echo "$output" | jq -r '.content[0].text' | jq -e '.[0].uriTemplate == "test://items/{id}"'
}

function synthetic_tools_disabled_by_config { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "res"
command = ["bash", "$FIXTURES_DIR/resource-server.bash"]
generate-resource-tools = false
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  local count
  count=$(echo "$output" | jq '[.tools[] | select(.name == "res-resource_read" or .name == "res-resource_templates")] | length')
  [[ "$count" -eq 0 ]]
}

function synthetic_tools_not_generated_for_non_resource_servers { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "test"
command = ["bash", "$FIXTURES_DIR/prompt-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  local count
  count=$(echo "$output" | jq '[.tools[] | select(.name == "test-resource_read" or .name == "test-resource_templates")] | length')
  [[ "$count" -eq 0 ]]
}
