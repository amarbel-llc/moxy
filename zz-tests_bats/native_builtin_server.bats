#! /usr/bin/env bats

# bats file_tags=native

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
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
  echo "$output" | jq -e '.tools[] | select(.name == "greeter.greet")' || fail ".tools[] | select(.name == \"greeter.greet\") check failed: $output"
  run jq -e '.tools[] | select(.name == "greeter.hello")' <<<"$output"
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

# write_greeter <parent-dir> <description>
write_greeter() {
  mkdir -p "$1/greeter"
  cat >"$1/greeter/_moxin.toml" <<EOF
schema = 1
name = "greeter"
description = "$2"
EOF
  cat >"$1/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello"
command = "echo"
args = ["-n", "hello"]
EOF
}

function status_marks_shadowed_moxin { # @test
  local dir_a="$BATS_TEST_TMPDIR/moxins-a"
  local dir_b="$BATS_TEST_TMPDIR/moxins-b"
  write_greeter "$dir_a" "local greeter"
  write_greeter "$dir_b" "shadowed greeter"

  mkdir -p "$HOME/project"
  cd "$HOME/project"

  export MOXIN_PATH="$dir_a:$dir_b"
  run_moxy status
  assert_output --partial "[shadowed by $dir_a]"
}

# The nix-built moxy bakes its moxy-moxins dir (which ships grit) in via
# ldflags, so grit appearing/disappearing in `moxy status` tracks whether
# the system dir was appended.
function status_appends_system_dir_after_explicit_moxin_path { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  write_greeter "$moxin_dir" "greeter"

  mkdir -p "$HOME/project"
  cd "$HOME/project"

  export MOXIN_PATH="$moxin_dir"
  run_moxy status
  assert_output --regexp $'\n +grit +[0-9]+ tools'
}

function status_honours_builtin_native_false { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  write_greeter "$moxin_dir" "greeter"

  mkdir -p "$HOME/project"
  cat >"$HOME/project/moxyfile" <<'EOF'
builtin-native = false
EOF
  cd "$HOME/project"

  export MOXIN_PATH="$moxin_dir"
  run_moxy status
  assert_output --partial "builtin-native = false: system moxin dir omitted"
  assert_output --regexp $'\n +greeter +[0-9]+ tools'
  refute_output --regexp $'\n +grit +[0-9]+ tools'
}
