#! /usr/bin/env bats

# bats file_tags=native

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

function broken_server_does_not_block_startup { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "broken"
command = "echo"
args = ["hello"]
EOF

  cd "$HOME/repo"
  run_moxy serve mcp
  assert_success
  assert_output --partial "failed to start broken"
}

function broken_server_exposes_status_tool { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "broken"
command = "echo"
args = ["hello"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "broken.status")'
}

function broken_server_status_tool_describes_error { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
[[servers]]
name = "broken"
command = "echo"
args = ["hello"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "broken.status") | .description | test("failed to start")'
}

function healthy_server_unaffected_by_broken_sibling { # @test
  command -v grit >/dev/null 2>&1 || skip "grit not in PATH"
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
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
  echo "$output" | jq -e '.tools[] | select(.name | startswith("grit."))' || fail ".tools[] | select(.name | startswith(\"grit.\")) check failed: $output"
  # broken.status is present
  echo "$output" | jq -e '.tools[] | select(.name == "broken.status")' || fail ".tools[] | select(.name == \"broken.status\") check failed: $output"
}

function all_servers_broken_still_starts { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
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
  # 2 status tools + builtin native server tools
  echo "$output" | jq -e '.tools | length > 1' || fail ".tools | length > 1 check failed: $output"
  echo "$output" | jq -e '.tools[] | select(.name == "broken-a.status")' || fail ".tools[] | select(.name == \"broken-a.status\") check failed: $output"
  echo "$output" | jq -e '.tools[] | select(.name == "broken-b.status")' || fail ".tools[] | select(.name == \"broken-b.status\") check failed: $output"
}
