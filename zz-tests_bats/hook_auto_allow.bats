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
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/greeter"
  cat >"$moxin_dir/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
description = "test server"
EOF
  cat >"$moxin_dir/greeter/hello.toml" <<'EOF'
schema = 1
perms-request = "always-allow"
description = "Say hello"
command = "echo"
args = ["-n", "hello"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  run_moxy_hook "mcp__moxy__greeter_hello"
  assert_success

  # Should output an allow decision
  echo "$output" | jq -e '.hookSpecificOutput.permissionDecision == "allow"'
  echo "$output" | jq -e '.hookSpecificOutput.hookEventName == "PreToolUse"'
}

function hook_falls_through_for_non_auto_allow_tool { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/greeter"
  cat >"$moxin_dir/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
description = "test server"
EOF
  cat >"$moxin_dir/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello"
command = "echo"
args = ["-n", "hello"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  run_moxy_hook "mcp__moxy__greeter_hello"
  assert_success

  # No output means fall-through (implicit allow via normal permission flow)
  [ -z "$output" ]
}

function hook_falls_through_for_builtin_tool { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  run_moxy_hook "Read"
  assert_success

  # No moxy prefix — falls through to go-mcp handler which also produces
  # no output (no tool mappings registered in this test).
  [ -z "$output" ]
}

function hook_allows_only_marked_tools { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/multi"
  cat >"$moxin_dir/multi/_moxin.toml" <<'EOF'
schema = 1
name = "multi"
description = "test server with mixed auto-allow"
EOF
  cat >"$moxin_dir/multi/safe.toml" <<'EOF'
schema = 1
perms-request = "always-allow"
description = "Auto-allowed"
command = "echo"
args = ["-n", "safe"]
EOF
  cat >"$moxin_dir/multi/dangerous.toml" <<'EOF'
schema = 1
description = "Not auto-allowed"
command = "echo"
args = ["-n", "dangerous"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  # safe should be allowed
  run_moxy_hook "mcp__moxy__multi_safe"
  assert_success
  echo "$output" | jq -e '.hookSpecificOutput.permissionDecision == "allow"'

  # dangerous should fall through
  run_moxy_hook "mcp__moxy__multi_dangerous"
  assert_success
  [ -z "$output" ]
}
