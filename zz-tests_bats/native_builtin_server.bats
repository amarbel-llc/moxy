#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function builtin_native_tool_appears_via_env_override { # @test
  # Create a builtin-servers dir with a simple native server config
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cat >"$builtin_dir/greeter.toml" <<'EOF'
name = "greeter"
description = "builtin greeter"

[[tools]]
name = "hello"
description = "Say hello"
command = "echo"
args = ["-n", "hello from builtin"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"

  export MOXY_BUILTIN_DIR="$builtin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "greeter.hello")'
}

function builtin_overridden_by_project_local { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cat >"$builtin_dir/greeter.toml" <<'EOF'
name = "greeter"
description = "builtin greeter"

[[tools]]
name = "hello"
description = "Say hello (builtin)"
command = "echo"
args = ["-n", "hello from builtin"]
EOF

  # Project-local override with a different tool name
  mkdir -p "$HOME/project/.moxy/servers"
  cat >"$HOME/project/.moxy/servers/greeter.toml" <<'EOF'
name = "greeter"
description = "local greeter"

[[tools]]
name = "greet"
description = "Greet (local override)"
command = "echo"
args = ["-n", "hello from local"]
EOF

  cd "$HOME/project"

  export MOXY_BUILTIN_DIR="$builtin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  # Local override should win: "greet" tool present, "hello" tool absent
  echo "$output" | jq -e '.tools[] | select(.name == "greeter.greet")'
  run bash -c "echo '$output' | jq -e '.tools[] | select(.name == \"greeter.hello\")'"
  assert_failure
}

function builtin_disabled_by_moxyfile { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cat >"$builtin_dir/greeter.toml" <<'EOF'
name = "greeter"
description = "builtin greeter"

[[tools]]
name = "hello"
description = "Say hello"
command = "echo"
args = ["-n", "hello from builtin"]
EOF

  mkdir -p "$HOME/project"
  cat >"$HOME/project/moxyfile" <<'EOF'
builtin-native = false
EOF

  cd "$HOME/project"

  export MOXY_BUILTIN_DIR="$builtin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  # No greeter tool should appear since builtins are disabled
  run bash -c "echo '$output' | jq -e '.tools[] | select(.name == \"greeter.hello\")'"
  assert_failure
}
