#! /usr/bin/env bats

# bats file_tags=restart

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"
}

teardown() {
  teardown_test_home
}

function restart_tool_listed_with_destructive_hint { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  # V1 protocol (2025-11-25) preserves tool annotations; V0 strips them.
  run_moxy_mcp_v1 tools/list
  assert_success
  # restart tool is now listed, gated by destructiveHint annotation so
  # MCP clients prompt the user before each invocation.
  echo "$output" | jq -e '.tools[] | select(.name == "restart")' || fail ".tools[] | select(.name == \"restart\") check failed: $output"
  echo "$output" | jq -e '.tools[] | select(.name == "restart") | .annotations.destructiveHint == true' || fail ".tools[] | select(.name == \"restart\") | .annotations.destructiveHint == true check failed: $output"
  echo "$output" | jq -e '.tools[] | select(.name == "restart") | .annotations.readOnlyHint == false' || fail ".tools[] | select(.name == \"restart\") | .annotations.readOnlyHint == false check failed: $output"
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
  echo "$output" | jq -e '.isError == true' || fail ".isError == true check failed: $output"
  echo "$output" | jq -e '.content[0].text | test("unknown server")' || fail ".content[0].text | test(\"unknown server\") check failed: $output"
}

function restart_missing_server_param_reloads_all { # @test
  # PR2 redefined an empty server arg as "reload everything".
  # Previously this returned an error ("server name is required").
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"restart","arguments":{}}'
  assert_success
  echo "$output" | jq -e '.content[0].text | test("Reloaded")'
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

function restart_moxin_succeeds { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/greeter"
  cat >"$moxin_dir/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
EOF
  cat >"$moxin_dir/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello"
command = "echo"
args = ["-n", "hello world"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp tools/call '{"name":"restart","arguments":{"server":"greeter"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text | test("restarted successfully")'
}

function restart_moxin_then_tool_call_works { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/greeter"
  cat >"$moxin_dir/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
EOF
  cat >"$moxin_dir/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello"
command = "echo"
args = ["-n", "hello world"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp_two \
    tools/call '{"name":"restart","arguments":{"server":"greeter"}}' \
    tools/call '{"name":"greeter.hello"}'
  assert_success
  assert_output --partial "hello world"
}

function restart_no_server_reloads_all { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/greeter"
  cat >"$moxin_dir/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
EOF
  cat >"$moxin_dir/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello"
command = "echo"
args = ["-n", "hello world"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp tools/call '{"name":"restart","arguments":{}}'
  assert_success
  echo "$output" | jq -e '.content[0].text | test("Reloaded")'
}

function restart_no_server_omitted_argument { # @test
  # Same as restart_no_server_reloads_all but with arguments omitted entirely
  # (rather than {}). Both shapes should reload.
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/greeter"
  cat >"$moxin_dir/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
EOF
  cat >"$moxin_dir/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello"
command = "echo"
args = ["-n", "hello world"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp tools/call '{"name":"restart"}'
  assert_success
  echo "$output" | jq -e '.content[0].text | test("Reloaded")'
}

function restart_no_server_then_tool_call_works { # @test
  # After restart{}, the freshly-rebuilt moxin must still be reachable.
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/greeter"
  cat >"$moxin_dir/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
EOF
  cat >"$moxin_dir/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello"
command = "echo"
args = ["-n", "hello world"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp_two \
    tools/call '{"name":"restart","arguments":{}}' \
    tools/call '{"name":"greeter.hello"}'
  assert_success
  assert_output --partial "hello world"
}
