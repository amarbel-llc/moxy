#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function builtin_native_tool_appears_via_moxin_path { # @test
  # Create a moxins dir with a simple moxin config
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/greeter"
  cat >"$moxin_dir/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
description = "builtin greeter"
EOF
  cat >"$moxin_dir/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello"
command = "echo"
args = ["-n", "hello from builtin"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"

  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "greeter.hello")'
}

function earlier_moxin_path_overrides_later { # @test
  local dir_a="$BATS_TEST_TMPDIR/moxins-a"
  local dir_b="$BATS_TEST_TMPDIR/moxins-b"

  # dir_b has "hello" tool
  mkdir -p "$dir_b/greeter"
  cat >"$dir_b/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
description = "builtin greeter"
EOF
  cat >"$dir_b/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello (builtin)"
command = "echo"
args = ["-n", "hello from builtin"]
EOF

  # dir_a overrides with "greet" tool (same server name)
  mkdir -p "$dir_a/greeter"
  cat >"$dir_a/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
description = "local greeter"
EOF
  cat >"$dir_a/greeter/greet.toml" <<'EOF'
schema = 1
description = "Greet (local override)"
command = "echo"
args = ["-n", "hello from local"]
EOF

  mkdir -p "$HOME/project"
  cd "$HOME/project"

  # A is earlier in path → A should win
  export MOXIN_PATH="$dir_a:$dir_b"
  run_moxy_mcp "tools/list"
  assert_success
  # Local override should win: "greet" tool present, "hello" tool absent
  echo "$output" | jq -e '.tools[] | select(.name == "greeter.greet")'
  run bash -c "echo '$output' | jq -e '.tools[] | select(.name == \"greeter.hello\")'"
  assert_failure
}

function builtin_disabled_by_moxyfile { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/greeter"
  cat >"$moxin_dir/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
description = "builtin greeter"
EOF
  cat >"$moxin_dir/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello"
command = "echo"
args = ["-n", "hello from builtin"]
EOF

  mkdir -p "$HOME/project"
  cat >"$HOME/project/moxyfile" <<'EOF'
builtin-native = false
EOF

  cd "$HOME/project"

  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  # greeter tool should still appear since MOXIN_PATH is set directly
  # (builtin-native only controls the system moxin dir appended automatically)
  echo "$output" | jq -e '.tools[] | select(.name == "greeter.hello")'
}
