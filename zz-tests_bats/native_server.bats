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
  run_moxy_mcp "tools/list"
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "greeter.hello")'
}

function native_server_tool_can_be_called { # @test
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
  local params='{"name":"greeter.hello"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "hello world"
}

function native_server_skipped_on_moxyfile_name_collision { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/myserver"
  cat >"$moxin_dir/myserver/_moxin.toml" <<'EOF'
schema = 1
name = "myserver"
EOF
  cat >"$moxin_dir/myserver/native-tool.toml" <<'EOF'
schema = 1
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
  mkdir -p "$moxin_dir/multi"
  cat >"$moxin_dir/multi/_moxin.toml" <<'EOF'
schema = 1
name = "multi"
EOF
  cat >"$moxin_dir/multi/first.toml" <<'EOF'
schema = 1
description = "First tool"
command = "echo"
args = ["-n", "one"]
EOF
  cat >"$moxin_dir/multi/second.toml" <<'EOF'
schema = 1
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

function native_server_content_type_sets_mimetype { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/typed"
  cat >"$moxin_dir/typed/_moxin.toml" <<'EOF'
schema = 1
name = "typed"
EOF
  cat >"$moxin_dir/typed/api.toml" <<'EOF'
schema = 1
command = "echo"
args = ["-n", "{\"ok\":true}"]
content-type = "application/json"
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"typed.api"}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  echo "$output" | jq -e '.content[0].type == "resource"'
  echo "$output" | jq -e '.content[0].resource.mimeType == "application/json"'
  echo "$output" | jq -e '.content[0].resource.text == "{\"ok\":true}"'
  echo "$output" | jq -e '.content[0].resource.uri | startswith("moxy.native://results/")'
}

function native_server_schema2_mcp_result_passthrough { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/s2"
  cat >"$moxin_dir/s2/_moxin.toml" <<'EOF'
schema = 1
name = "s2"
EOF
  cat >"$moxin_dir/s2/api.toml" <<'EOF'
schema = 2
command = "echo"
args = ["-n", "{\"content\":[{\"type\":\"text\",\"text\":\"hello from mcp\",\"mimeType\":\"text/plain\"}]}"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"s2.api"}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  echo "$output" | jq -e '.content[0].type == "resource"'
  echo "$output" | jq -e '.content[0].resource.text == "hello from mcp"'
  echo "$output" | jq -e '.content[0].resource.mimeType == "text/plain"'
}

function native_server_schema2_nonzero_exit_ignores_stdout { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/s2fail"
  cat >"$moxin_dir/s2fail/_moxin.toml" <<'EOF'
schema = 1
name = "s2fail"
EOF
  cat >"$moxin_dir/s2fail/bad.toml" <<'EOF'
schema = 2
command = "sh"
args = ["-c", "echo '{\"content\":[{\"type\":\"text\",\"text\":\"should be ignored\"}]}'; exit 1"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"s2fail.bad"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  echo "$output" | jq -e '.isError == true'
}

function native_server_schema2_iserror_respected { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/s2err"
  cat >"$moxin_dir/s2err/_moxin.toml" <<'EOF'
schema = 1
name = "s2err"
EOF
  cat >"$moxin_dir/s2err/err.toml" <<'EOF'
schema = 2
command = "echo"
args = ["-n", "{\"content\":[{\"type\":\"text\",\"text\":\"tool error\"}],\"isError\":true}"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"s2err.err"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  echo "$output" | jq -e '.isError == true'
  echo "$output" | jq -e '.content[0].text == "tool error"'
}

function native_server_schema2_invalid_json_returns_error { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/s2bad"
  cat >"$moxin_dir/s2bad/_moxin.toml" <<'EOF'
schema = 1
name = "s2bad"
EOF
  cat >"$moxin_dir/s2bad/broken.toml" <<'EOF'
schema = 2
command = "echo"
args = ["-n", "not json at all"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"s2bad.broken"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  echo "$output" | jq -e '.isError == true'
  assert_output --partial "invalid MCP result JSON"
}

function native_server_schema2_text_mode { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/s2text"
  cat >"$moxin_dir/s2text/_moxin.toml" <<'EOF'
schema = 1
name = "s2text"
EOF
  cat >"$moxin_dir/s2text/plain.toml" <<'EOF'
schema = 2
command = "echo"
args = ["-n", "just plain text"]
result-type = "text"
content-type = "text/csv"
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  local params='{"name":"s2text.plain"}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  echo "$output" | jq -e '.content[0].type == "resource"'
  echo "$output" | jq -e '.content[0].resource.text == "just plain text"'
  echo "$output" | jq -e '.content[0].resource.mimeType == "text/csv"'
}
