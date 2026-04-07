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

function child_tool_title_is_prefixed_with_server_name { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/titled-tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp_v1 tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "srv.update_thing") | .title == "srv: Update Thing"'
}

function child_tool_annotation_title_is_prefixed { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/titled-tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp_v1 tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "srv.update_thing") | .annotations.title == "srv: Update Thing"'
}

function child_tool_without_title_gets_no_title { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp_v1 tools/list
  assert_success
  # tool-server.bash tools have no title, should remain null/absent
  echo "$output" | jq -e '.tools[] | select(.name == "srv.execute-command") | .title == null'
}

function moxy_builtin_restart_has_title { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp_v1 tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "restart") | .title == "Restart Server"'
}

function moxy_builtin_exec_mcp_has_title { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp_v1 tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "exec-mcp") | .title == "Execute Tool on Server"'
}

function moxy_builtin_restart_has_annotation_title { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp_v1 tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "restart") | .annotations.title == "Restart Server"'
}
