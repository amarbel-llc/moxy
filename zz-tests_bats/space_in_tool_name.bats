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

function tool_with_space_in_name_is_listed { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/space-tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "srv.my tool")'
}

function tool_with_space_in_name_can_be_called { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/space-tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"srv.my tool","arguments":{"arg":"hello"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "got: hello"'
}
