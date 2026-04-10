#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function native_server_tool_appears_in_tools_list { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cat >"$moxin_dir/greeter.toml" <<'EOF'
name = "greeter"

[[tools]]
name = "hello"
description = "Say hello"
command = "echo"
args = ["-n", "hello world"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "greeter.hello")'
}

function native_server_tool_can_be_called { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cat >"$moxin_dir/greeter.toml" <<'EOF'
name = "greeter"

[[tools]]
name = "hello"
description = "Say hello"
command = "echo"
args = ["-n", "hello world"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"greeter.hello"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "hello world"
}

function native_server_skipped_on_moxyfile_name_collision { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cat >"$moxin_dir/myserver.toml" <<'EOF'
name = "myserver"

[[tools]]
name = "native-tool"
description = "From native config"
command = "echo"
args = ["-n", "native"]
EOF

  mkdir -p "$HOME/project"
  cat >"$HOME/project/moxyfile" <<'EOF'
[[servers]]
name = "myserver"
command = "echo"
args = ["moxyfile-server"]
EOF

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp_with_stderr "tools/list"
  assert_success
  # The native tool should NOT appear (moxyfile server wins).
  # The moxyfile server will fail to start (echo exits immediately),
  # so we get a status tool instead.
  echo "$output" | jq -e '.tools[] | select(.name == "myserver.status")'
  # Verify native tool is not present
  run bash -c "echo '$output' | jq -e '.tools[] | select(.name == \"myserver.native-tool\")'"
  assert_failure
}

function native_server_multiple_tools { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cat >"$moxin_dir/multi.toml" <<'EOF'
name = "multi"

[[tools]]
name = "first"
description = "First tool"
command = "echo"
args = ["-n", "one"]

[[tools]]
name = "second"
description = "Second tool"
command = "echo"
args = ["-n", "two"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "multi.first")'
  echo "$output" | jq -e '.tools[] | select(.name == "multi.second")'
}
