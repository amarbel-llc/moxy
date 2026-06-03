#! /usr/bin/env bats

# bats file_tags=native

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"
}

teardown() {
  teardown_test_home
}

function initialize_instructions_contain_server_summary { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp_init
  assert_success
  # Instructions should mention the server name
  echo "$output" | jq -e '.instructions | test("srv")' || fail '.instructions | test("srv") check failed: '"$output"
  # Instructions should mention tool count
  echo "$output" | jq -e '.instructions | test("1 tools")' || fail '.instructions | test("1 tools") check failed: '"$output"
}

function initialize_instructions_contain_failed_server { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "broken"
command = "echo"
args = ["hello"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp_init
  assert_success
  echo "$output" | jq -e '.instructions | test("broken")' || fail '.instructions | test("broken") check failed: '"$output"
  echo "$output" | jq -e '.instructions | test("failed")' || fail '.instructions | test("failed") check failed: '"$output"
}

function initialize_instructions_contain_resource_counts { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "combo"
command = ["bash", "$FIXTURES_DIR/tool-and-resource-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp_init
  assert_success
  echo "$output" | jq -e '.instructions | test("2 tools")' || fail '.instructions | test("2 tools") check failed: '"$output"
  echo "$output" | jq -e '.instructions | test("1 resources")' || fail '.instructions | test("1 resources") check failed: '"$output"
  echo "$output" | jq -e '.instructions | test("1 resource templates")' || fail '.instructions | test("1 resource templates") check failed: '"$output"
}

function moxy_servers_resource_lists_all_servers { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"moxy://servers"}'
  assert_success
  local server_json
  server_json=$(echo "$output" | jq -r '.contents[0].text')
  echo "$server_json" | jq -e '.[0].name == "srv"' || fail '.[0].name == "srv" check failed: '"$server_json"
  echo "$server_json" | jq -e '.[0].status == "running"' || fail '.[0].status == "running" check failed: '"$server_json"
  echo "$server_json" | jq -e '.[0].tools == 1' || fail '.[0].tools == 1 check failed: '"$server_json"
}

function moxy_servers_resource_includes_failed_server { # @test
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
  run_moxy_mcp resources/read '{"uri":"moxy://servers"}'
  assert_success
  local server_json
  server_json=$(echo "$output" | jq -r '.contents[0].text')
  echo "$server_json" | jq -e '.[] | select(.name == "broken") | .status == "failed"' || fail '.[] | select(.name == "broken") | .status == "failed" check failed: '"$server_json"
  echo "$server_json" | jq -e '.[] | select(.name == "broken") | .error | length > 0' || fail '.[] | select(.name == "broken") | .error | length > 0 check failed: '"$server_json"
}

function moxy_servers_single_server_resource { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"moxy://servers/srv"}'
  assert_success
  local server_json
  server_json=$(echo "$output" | jq -r '.contents[0].text')
  echo "$server_json" | jq -e '.name == "srv"' || fail '.name == "srv" check failed: '"$server_json"
  echo "$server_json" | jq -e '.status == "running"' || fail '.status == "running" check failed: '"$server_json"
}

function moxy_servers_resource_appears_in_resources_list { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/list
  assert_success
  echo "$output" | jq -e '.resources[] | select(.uri == "moxy://servers")' || fail '.resources[] | select(.uri == "moxy://servers") check failed: '"$output"
}

function moxy_servers_template_appears_in_templates_list { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/templates/list
  assert_success
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "moxy://servers/{server}")' || fail '.resourceTemplates[] | select(.uriTemplate == "moxy://servers/{server}") check failed: '"$output"
}

function resource_read_unknown_server_returns_hint { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp resources/read '{"uri":"unknown/some-resource"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | jq -e '.error' || fail '.error check failed: '"$text"
  echo "$text" | jq -e '.hint | test("moxy://servers")' || fail '.hint | test("moxy://servers") check failed: '"$text"
}

function resource_read_server_no_resources_returns_hint { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  # srv has tools but no resources — trying to read a resource should hint
  run_moxy_mcp resources/read '{"uri":"srv/nonexistent://resource"}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | jq -e '.error' || fail '.error check failed: '"$text"
  echo "$text" | jq -e '.hint | test("moxy://tools/srv")' || fail '.hint | test("moxy://tools/srv") check failed: '"$text"
}
