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
  echo "$output" | jq -e '.instructions | test("srv")'
  # Instructions should mention tool count
  echo "$output" | jq -e '.instructions | test("1 tools")'
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
  echo "$output" | jq -e '.instructions | test("broken")'
  echo "$output" | jq -e '.instructions | test("failed")'
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
  echo "$output" | jq -e '.instructions | test("2 tools")'
  echo "$output" | jq -e '.instructions | test("1 resources")'
  echo "$output" | jq -e '.instructions | test("1 resource templates")'
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
  echo "$server_json" | jq -e '.[0].name == "srv"'
  echo "$server_json" | jq -e '.[0].status == "running"'
  echo "$server_json" | jq -e '.[0].tools == 1'
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
  echo "$server_json" | jq -e '.[] | select(.name == "broken") | .status == "failed"'
  echo "$server_json" | jq -e '.[] | select(.name == "broken") | .error | length > 0'
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
  echo "$server_json" | jq -e '.name == "srv"'
  echo "$server_json" | jq -e '.status == "running"'
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
  echo "$output" | jq -e '.resources[] | select(.uri == "moxy://servers")'
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
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "moxy://servers/{server}")'
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
  echo "$text" | jq -e '.error'
  echo "$text" | jq -e '.hint | test("moxy://servers")'
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
  echo "$text" | jq -e '.error'
  echo "$text" | jq -e '.hint | test("moxy://tools/srv")'
}
