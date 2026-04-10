#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

# Helper: invoke moxy hook with a tool_name and capture stdout.
run_moxy_hook() {
  local tool_name="$1"
  local hook_input
  hook_input=$(jq -cn --arg tn "$tool_name" '{
    hook_event_name: "PreToolUse",
    tool_name: $tn,
    tool_input: {}
  }')
  run timeout --preserve-status "5s" bash -c \
    'echo "$1" | moxy hook' -- "$hook_input"
}

function hook_allows_auto_allow_tool { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cat >"$builtin_dir/greeter.toml" <<'EOF'
name = "greeter"
description = "test server"

[[tools]]
name = "hello"
auto-allow = true
description = "Say hello"
command = "echo"
args = ["-n", "hello"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  run_moxy_hook "mcp__moxy__greeter_hello"
  assert_success

  # Should output an allow decision
  echo "$output" | jq -e '.hookSpecificOutput.permissionDecision == "allow"'
  echo "$output" | jq -e '.hookSpecificOutput.hookEventName == "PreToolUse"'
}

function hook_falls_through_for_non_auto_allow_tool { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cat >"$builtin_dir/greeter.toml" <<'EOF'
name = "greeter"
description = "test server"

[[tools]]
name = "hello"
description = "Say hello"
command = "echo"
args = ["-n", "hello"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  run_moxy_hook "mcp__moxy__greeter_hello"
  assert_success

  # No output means fall-through (implicit allow via normal permission flow)
  [ -z "$output" ]
}

function hook_falls_through_for_builtin_tool { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  run_moxy_hook "Read"
  assert_success

  # No moxy prefix — falls through to go-mcp handler which also produces
  # no output (no tool mappings registered in this test).
  [ -z "$output" ]
}

function hook_allows_only_marked_tools { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cat >"$builtin_dir/multi.toml" <<'EOF'
name = "multi"
description = "test server with mixed auto-allow"

[[tools]]
name = "safe"
auto-allow = true
description = "Auto-allowed"
command = "echo"
args = ["-n", "safe"]

[[tools]]
name = "dangerous"
description = "Not auto-allowed"
command = "echo"
args = ["-n", "dangerous"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  # safe should be allowed
  run_moxy_hook "mcp__moxy__multi_safe"
  assert_success
  echo "$output" | jq -e '.hookSpecificOutput.permissionDecision == "allow"'

  # dangerous should fall through
  run_moxy_hook "mcp__moxy__multi_dangerous"
  assert_success
  [ -z "$output" ]
}
