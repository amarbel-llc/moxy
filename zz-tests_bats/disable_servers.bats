#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function disable_servers_omits_named_server { # @test
  # A server declared in a higher-priority moxyfile can be disabled by a
  # lower-priority moxyfile via disable-servers. The disabled server should
  # not be spawned, so its tools must not appear in tools/list.
  mkdir -p "$HOME/project"
  cat >"$HOME/project/moxyfile" <<EOF
disable-servers = ["myecho"]

[[servers]]
name = "myecho"
command = ["bash", "$BATS_TEST_DIRNAME/test-fixtures/tool-server.bash"]
EOF

  cd "$HOME/project"
  run_moxy_mcp "tools/list"
  assert_success
  # myecho.execute-command should NOT appear (server disabled)
  run bash -c "echo '$output' | jq -e '.tools[] | select(.name == \"myecho.execute-command\")'"
  assert_failure
}

function disable_servers_keeps_undisabled_servers { # @test
  # Disabling one server should leave others alone.
  mkdir -p "$HOME/project"
  cat >"$HOME/project/moxyfile" <<EOF
disable-servers = ["a"]

[[servers]]
name = "a"
command = ["bash", "$BATS_TEST_DIRNAME/test-fixtures/tool-server.bash"]

[[servers]]
name = "b"
command = ["bash", "$BATS_TEST_DIRNAME/test-fixtures/tool-server.bash"]
EOF

  cd "$HOME/project"
  run_moxy_mcp "tools/list"
  assert_success
  local tools_output="$output"
  # a should be omitted
  run bash -c "echo '$tools_output' | jq -e '.tools[] | select(.name == \"a.execute-command\")'"
  assert_failure
  # b should still appear
  echo "$tools_output" | jq -e '.tools[] | select(.name == "b.execute-command")'
}

function disable_servers_hierarchy_additive { # @test
  # Like disable-moxins, disable-servers merges additively across the
  # hierarchy: global disables one, project disables another, both omitted.
  mkdir -p "$HOME/.config/moxy"
  cat >"$HOME/.config/moxy/moxyfile" <<EOF
disable-servers = ["a"]

[[servers]]
name = "a"
command = ["bash", "$BATS_TEST_DIRNAME/test-fixtures/tool-server.bash"]

[[servers]]
name = "b"
command = ["bash", "$BATS_TEST_DIRNAME/test-fixtures/tool-server.bash"]
EOF

  mkdir -p "$HOME/project"
  cat >"$HOME/project/moxyfile" <<EOF
disable-servers = ["b"]
EOF

  cd "$HOME/project"
  run_moxy_mcp "tools/list"
  assert_success
  local tools_output="$output"
  run bash -c "echo '$tools_output' | jq -e '.tools[] | select(.name == \"a.execute-command\")'"
  assert_failure
  run bash -c "echo '$tools_output' | jq -e '.tools[] | select(.name == \"b.execute-command\")'"
  assert_failure
}

function disable_servers_does_not_affect_moxins { # @test
  # disable-servers only filters [[servers]] entries; moxins discovered via
  # MOXIN_PATH should be unaffected.
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
  # Disable "greeter" via disable-servers (which only affects [[servers]]).
  # The greeter moxin should still appear since disable-servers doesn't
  # touch moxins.
  cat >"$HOME/project/moxyfile" <<EOF
disable-servers = ["greeter"]

[[servers]]
name = "myecho"
command = ["bash", "$BATS_TEST_DIRNAME/test-fixtures/tool-server.bash"]
EOF

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"
  run_moxy_mcp "tools/list"
  assert_success
  # greeter.hello should still appear (it's a moxin, not a moxyfile server)
  echo "$output" | jq -e '.tools[] | select(.name == "greeter.hello")'
  # myecho should still appear (not in disable-servers list)
  echo "$output" | jq -e '.tools[] | select(.name == "myecho.execute-command")'
}
