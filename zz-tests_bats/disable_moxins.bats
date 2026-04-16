#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function disable_moxins_whole_server_omitted_from_tools_list { # @test
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
  cat >"$HOME/project/moxyfile" <<'EOF'
disable-moxins = ["greeter"]
EOF

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  # greeter.hello should NOT appear
  run bash -c "echo '$output' | jq -e '.tools[] | select(.name == \"greeter.hello\")'"
  assert_failure
}

function disable_moxins_individual_tool_omitted { # @test
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
  cat >"$HOME/project/moxyfile" <<'EOF'
disable-moxins = ["multi.first"]
EOF

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  local tools_output="$output"
  # multi.first should be omitted
  run bash -c "echo '$tools_output' | jq -e '.tools[] | select(.name == \"multi.first\")'"
  assert_failure
  # multi.second should still be present
  echo "$tools_output" | jq -e '.tools[] | select(.name == "multi.second")'
}

function disable_moxins_hierarchy_additive { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/alpha"
  cat >"$moxin_dir/alpha/_moxin.toml" <<'EOF'
schema = 1
name = "alpha"
EOF
  cat >"$moxin_dir/alpha/tool.toml" <<'EOF'
schema = 1
description = "Alpha tool"
command = "echo"
args = ["-n", "alpha"]
EOF

  mkdir -p "$moxin_dir/beta"
  cat >"$moxin_dir/beta/_moxin.toml" <<'EOF'
schema = 1
name = "beta"
EOF
  cat >"$moxin_dir/beta/tool.toml" <<'EOF'
schema = 1
description = "Beta tool"
command = "echo"
args = ["-n", "beta"]
EOF

  # Global disables alpha
  mkdir -p "$HOME/.config/moxy"
  cat >"$HOME/.config/moxy/moxyfile" <<'EOF'
disable-moxins = ["alpha"]
EOF

  # Project disables beta
  mkdir -p "$HOME/project"
  cat >"$HOME/project/moxyfile" <<'EOF'
disable-moxins = ["beta"]
EOF

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  # Both should be omitted (additive merge)
  run bash -c "echo '$output' | jq -e '.tools[] | select(.name == \"alpha.tool\")'"
  assert_failure
  run bash -c "echo '$output' | jq -e '.tools[] | select(.name == \"beta.tool\")'"
  assert_failure
}

function disable_moxins_does_not_affect_moxyfile_servers { # @test
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
args = ["-n", "hello"]
EOF

  mkdir -p "$HOME/project"
  # Disable "myserver" which is a moxyfile server, not a moxin.
  # The moxyfile server should still appear (disable-moxins only affects moxins).
  cat >"$HOME/project/moxyfile" <<'EOF'
disable-moxins = ["myserver"]

[[servers]]
name = "myserver"
command = "echo"
EOF

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  # myserver should still appear (it's a moxyfile server, not a moxin)
  # It will fail to start, so we get a status tool
  echo "$output" | jq -e '.tools[] | select(.name == "myserver.status")'
  # greeter should still appear (not in disable list)
  echo "$output" | jq -e '.tools[] | select(.name == "greeter.hello")'
}
