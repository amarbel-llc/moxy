#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function broken_server_does_not_block_startup { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "broken"
command = "echo"
args = ["hello"]
EOF

  cd "$HOME/repo"
  run_moxy
  assert_success
  assert_output --partial "failed to start broken"
}

function broken_server_exposes_status_tool { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "broken"
command = "echo"
args = ["hello"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "broken-status")'
}

function broken_server_status_tool_describes_error { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "broken"
command = "echo"
args = ["hello"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "broken-status") | .description | test("failed to start")'
}

function healthy_server_unaffected_by_broken_sibling { # @test
  command -v grit >/dev/null 2>&1 || skip "grit not in PATH"
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "grit"
command = "grit"

[[servers]]
name = "broken"
command = "echo"
args = ["hello"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  # grit tools are present
  echo "$output" | jq -e '.tools[] | select(.name | startswith("grit-"))'
  # broken-status is present
  echo "$output" | jq -e '.tools[] | select(.name == "broken-status")'
}

function all_servers_broken_still_starts { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "broken-a"
command = "echo"
args = ["hello"]

[[servers]]
name = "broken-b"
command = "echo"
args = ["goodbye"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  # 2 status tools + 1 restart tool = 3
  echo "$output" | jq -e '.tools | length == 3'
  echo "$output" | jq -e '.tools[] | select(.name == "broken-a-status")'
  echo "$output" | jq -e '.tools[] | select(.name == "broken-b-status")'
  echo "$output" | jq -e '.tools[] | select(.name == "restart")'
}
