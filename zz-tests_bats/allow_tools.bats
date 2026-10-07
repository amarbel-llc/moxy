#! /usr/bin/env bats

# bats file_tags=allow_tools

# The moxyfile `allow-tools` allowlist: only listed tools are advertised or
# callable, meta tools included; layers of the hierarchy intersect; and the
# narrowing relation covers it. See moxyfile(5).

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"
  export MOXIN_PATH="$BATS_TEST_TMPDIR/no-moxins"
  mkdir -p "$MOXIN_PATH" "$HOME/repo" "$HOME/.config/moxy"
}

teardown() {
  teardown_test_home
}

# Two servers, each exposing `execute-command`, with the system moxins off so
# the full surface is exactly those two tools plus moxy's meta tools.
write_two_servers() {
  cat >"$1" <<EOF
builtin-native = false
$2

[[servers]]
name = "one"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]

[[servers]]
name = "two"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF
}

tool_names() {
  echo "$output" | jq -c '[(.tools // [])[].name] | sort'
}

function without_an_allowlist_everything_is_listed { # @test
  write_two_servers "$HOME/repo/moxyfile" ""
  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  echo "$output" | jq -e '[.tools[].name] | index("one.execute-command") and index("two.execute-command") and index("restart")' ||
    fail "expected both servers and the meta tools: $(tool_names)"
}

function an_allowlist_leaves_only_what_it_names { # @test
  write_two_servers "$HOME/repo/moxyfile" 'allow-tools = ["one.execute-command"]'
  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  assert_equal "$(tool_names)" '["one.execute-command"]'
}

function a_bare_name_allows_a_whole_server_and_a_meta_tool_by_its_own_name { # @test
  write_two_servers "$HOME/repo/moxyfile" 'allow-tools = ["two", "restart"]'
  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  assert_equal "$(tool_names)" '["restart","two.execute-command"]'
}

function an_empty_allowlist_leaves_nothing { # @test
  write_two_servers "$HOME/repo/moxyfile" 'allow-tools = []'
  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  assert_equal "$(tool_names)" '[]'
}

function an_unlisted_tool_is_uncallable_not_merely_unlisted { # @test
  write_two_servers "$HOME/repo/moxyfile" 'allow-tools = ["one.execute-command"]'
  cd "$HOME/repo"

  run_moxy_mcp tools/call '{"name":"two.execute-command","arguments":{"cmd":"x"}}'
  assert_success
  echo "$output" | jq -e '.isError == true and (.content[0].text | test("allow-tools"))' ||
    fail "unlisted server tool was callable: $output"

  run_moxy_mcp tools/call '{"name":"restart","arguments":{}}'
  assert_success
  echo "$output" | jq -e '.isError == true and (.content[0].text | test("allow-tools"))' ||
    fail "unlisted meta tool was callable: $output"

  run_moxy_mcp tools/call '{"name":"one.execute-command","arguments":{"cmd":"hello"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "executed: hello"' ||
    fail "listed tool was not callable: $output"
}

function a_closer_moxyfile_can_only_take_tools_away { # @test
  # The global file allows server one and restart; the project file asks for
  # one tool of one, plus server two and batch, which the global never
  # allowed. Only what both allow survives.
  cat >"$HOME/.config/moxy/moxyfile" <<'EOF'
allow-tools = ["one", "restart"]
EOF
  write_two_servers "$HOME/repo/moxyfile" 'allow-tools = ["one.execute-command", "two", "batch"]'
  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  assert_equal "$(tool_names)" '["one.execute-command"]'

  run_moxy show-effective-moxyfile
  assert_success
  assert_output --partial 'allow-tools = ["one.execute-command"]'
}

function narrows_judges_the_allowlist { # @test
  mkdir -p "$HOME/parent" "$HOME/child"
  write_two_servers "$HOME/parent/p.toml" 'allow-tools = ["one", "two.execute-command"]'

  write_two_servers "$HOME/child/c.toml" 'allow-tools = ["one.execute-command"]'
  run_moxy narrows --parent "$HOME/parent/p.toml" --child "$HOME/child/c.toml"
  assert_success
  assert_output ""

  write_two_servers "$HOME/child/c.toml" 'allow-tools = ["two", "restart", "one"]'
  run_moxy narrows --parent "$HOME/parent/p.toml" --child "$HOME/child/c.toml"
  assert_failure 1
  assert_output "allow-tools: child allows restart, parent does not
allow-tools: child allows two, parent does not"

  write_two_servers "$HOME/child/c.toml" ""
  run_moxy narrows --parent "$HOME/parent/p.toml" --child "$HOME/child/c.toml"
  assert_failure 1
  assert_output "allow-tools: parent sets an allowlist, child does not"
}
