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

function prompts_list_returns_prefixed_names { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "test"
command = ["bash", "$FIXTURES_DIR/prompt-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp prompts/list
  assert_success
  echo "$output" | jq -e '.prompts[] | select(.name == "test.greet")'
}

function prompts_list_preserves_description { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "test"
command = ["bash", "$FIXTURES_DIR/prompt-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp prompts/list
  assert_success
  echo "$output" | jq -e '.prompts[] | select(.name == "test.greet") | .description == "Generate a greeting"'
}

function prompts_list_preserves_arguments { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "test"
command = ["bash", "$FIXTURES_DIR/prompt-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp prompts/list
  assert_success
  echo "$output" | jq -e '.prompts[] | select(.name == "test.greet") | .arguments[0].name == "name"'
}

function prompts_get_dispatches_to_child { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "test"
command = ["bash", "$FIXTURES_DIR/prompt-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp prompts/get '{"name":"test.greet","arguments":{"name":"Alice"}}'
  assert_success
  echo "$output" | jq -e '.messages[0].content.text == "Hello, Alice!"'
}

function prompts_list_skips_servers_without_capability { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "broken"
command = ["echo", "hello"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp prompts/list
  assert_success
  echo "$output" | jq -e '.prompts | length == 0'
}
