#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function exec_no_rules_allows_everything { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"exec","arguments":{"command":"echo hello"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "hello\n"'
}

function exec_allow_rule_permits_matching_command { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
[[exec.allow]]
binary = "echo"
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"exec","arguments":{"command":"echo allowed"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "allowed\n"'
}

function exec_allow_rule_denies_unmatched_binary { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
[[exec.allow]]
binary = "echo"
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"exec","arguments":{"command":"ls /tmp"}}'
  assert_success
  echo "$output" | jq -e '.isError == true'
  echo "$output" | jq -e '.content[0].text | test("no allow rule")'
}

function exec_deny_rule_blocks_matching_command { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
[[exec.deny]]
binary = "rm"
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"exec","arguments":{"command":"rm -rf /tmp/test"}}'
  assert_success
  echo "$output" | jq -e '.isError == true'
  echo "$output" | jq -e '.content[0].text | test("deny rule")'
}

function exec_deny_wins_over_allow { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
[[exec.allow]]
binary = "git"

[[exec.deny]]
binary = "git"
args = ["push --force"]
EOF

  cd "$HOME/repo"
  # Allowed subcommand.
  run_moxy_mcp tools/call '{"name":"exec","arguments":{"command":"git --version"}}'
  assert_success
  echo "$output" | jq -e '.isError // false | not'

  # Denied subcommand.
  run_moxy_mcp tools/call '{"name":"exec","arguments":{"command":"git push --force origin master"}}'
  assert_success
  echo "$output" | jq -e '.isError == true'
}

function exec_allow_with_args_restricts_subcommands { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
[[exec.allow]]
binary = "git"
args = ["--version", "diff"]
EOF

  cd "$HOME/repo"
  # Allowed subcommand.
  run_moxy_mcp tools/call '{"name":"exec","arguments":{"command":"git --version"}}'
  assert_success
  echo "$output" | jq -e '.isError // false | not'

  # Denied subcommand — binary allowed but args don't match.
  run_moxy_mcp tools/call '{"name":"exec","arguments":{"command":"git push"}}'
  assert_success
  echo "$output" | jq -e '.isError == true'
}

function exec_empty_stdout_returns_empty_content { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"exec","arguments":{"command":"true"}}'
  assert_success

  # Empty stdout must not produce a content block with an empty text field,
  # because omitempty drops it and MCP clients reject {"type":"text"} without
  # a "text" key. Return empty/null content instead.
  echo "$output" | jq -e '(.content // []) | length == 0'
}

function exec_deny_only_allows_other_binaries { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<'EOF'
[[exec.deny]]
binary = "sudo"
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"exec","arguments":{"command":"echo works"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "works\n"'
}
