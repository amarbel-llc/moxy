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
  assert_output "allow-tools: child holds restart at use, parent at none
allow-tools: child holds two at use, parent at none"

  write_two_servers "$HOME/child/c.toml" ""
  run_moxy narrows --parent "$HOME/parent/p.toml" --child "$HOME/child/c.toml"
  assert_failure 1
  assert_output "allow-tools: parent sets an allowlist, child does not"
}

# pass-tools: tools the holder may confer on a child but never calls.

function a_pass_tool_is_neither_listed_nor_callable { # @test
  write_two_servers "$HOME/repo/moxyfile" 'allow-tools = ["one.execute-command"]
pass-tools = ["two", "restart"]'
  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  assert_equal "$(tool_names)" '["one.execute-command"]'

  run_moxy_mcp tools/call '{"name":"two.execute-command","arguments":{"cmd":"x"}}'
  assert_success
  echo "$output" | jq -e '.isError == true and (.content[0].text | test("allow-tools"))' ||
    fail "pass tool was callable: $output"

  run_moxy_mcp tools/call '{"name":"restart","arguments":{}}'
  assert_success
  echo "$output" | jq -e '.isError == true and (.content[0].text | test("allow-tools"))' ||
    fail "pass meta tool was callable: $output"
}

function a_pass_tool_is_not_callable_through_batch { # @test
  # The sub-call must clear batch's permission preflight (a [[servers]] tool
  # has no perms-request and would bail out there), so the pass tool is a
  # one-tool moxin that asks for nothing: only allow-tools can refuse it.
  mkdir -p "$MOXIN_PATH/greeter"
  cat >"$MOXIN_PATH/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
EOF
  cat >"$MOXIN_PATH/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello"
perms-request = "always-allow"
command = "echo"
args = ["-n", "hello"]
EOF
  write_two_servers "$HOME/repo/moxyfile" 'allow-tools = ["one.execute-command", "batch"]
pass-tools = ["greeter"]'
  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"batch","arguments":{"calls":[{"tool":"greeter.hello","args":{}}]}}'
  assert_success
  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  # Past the preflight, refused at dispatch by allow-tools.
  echo "$text" | jq -se 'map(select(.type == "bailout")) | length == 0' ||
    fail "batch bailed out in preflight instead of reaching dispatch: $text"
  echo "$text" | jq -e 'select(.type == "test") | tostring | test("allow-tools")' ||
    fail "pass tool was not refused by allow-tools inside batch: $text"
  echo "$text" | jq -e 'select(.type == "summary") | .passed == 0' ||
    fail "the pass tool ran inside batch: $text"
}

function pass_tools_without_an_allowlist_is_refused { # @test
  write_two_servers "$HOME/repo/moxyfile" 'pass-tools = ["two"]'
  cd "$HOME/repo"
  run_moxy serve mcp
  assert_failure
  assert_output --partial "pass-tools is set without allow-tools"
}

function a_tool_held_at_both_levels_is_refused { # @test
  write_two_servers "$HOME/repo/moxyfile" 'allow-tools = ["two"]
pass-tools = ["two.execute-command"]'
  cd "$HOME/repo"
  run_moxy serve mcp
  assert_failure
  assert_output --partial 'pass-tools: "two.execute-command" is also allowed by allow-tools'
}

function narrows_lets_a_child_use_what_its_parent_passes { # @test
  mkdir -p "$HOME/parent" "$HOME/child"
  write_two_servers "$HOME/parent/p.toml" 'allow-tools = ["one.execute-command"]
pass-tools = ["two"]'

  # Conferring use from a pass holder is the one upward step.
  write_two_servers "$HOME/child/c.toml" 'allow-tools = ["two.execute-command"]'
  run_moxy narrows --parent "$HOME/parent/p.toml" --child "$HOME/child/c.toml"
  assert_success
  assert_output ""

  # Pass delegates under the same rule.
  write_two_servers "$HOME/child/c.toml" 'allow-tools = []
pass-tools = ["two.execute-command", "one.execute-command"]'
  run_moxy narrows --parent "$HOME/parent/p.toml" --child "$HOME/child/c.toml"
  assert_success
  assert_output ""

  # What the parent holds at neither level is refused, at either level, with
  # the level it would have reached.
  write_two_servers "$HOME/child/c.toml" 'allow-tools = ["one", "restart"]
pass-tools = ["batch"]'
  run_moxy narrows --parent "$HOME/parent/p.toml" --child "$HOME/child/c.toml"
  assert_failure 1
  assert_output "allow-tools: child holds one at use, parent at none
allow-tools: child holds restart at use, parent at none
pass-tools: child holds batch at pass, parent at none"

  run_moxy narrows --json --parent "$HOME/parent/p.toml" --child "$HOME/child/c.toml"
  assert_failure 1
  echo "$output" | jq -e '.violations[2] == {"key":"pass-tools","subject":"batch","message":"child holds batch at pass, parent at none"}' ||
    fail "violation shape: $output"
}

function a_child_serves_a_tool_its_parent_reference_passes { # @test
  mkdir -p "$HOME/parent" "$HOME/child"
  write_two_servers "$HOME/parent/moxyfile" 'allow-tools = ["one.execute-command"]
pass-tools = ["two.execute-command"]'
  write_two_servers "$HOME/child/moxyfile" 'allow-tools = ["two.execute-command"]'
  export MOXY_PARENT_MOXYFILE="$HOME/parent/moxyfile"
  cd "$HOME/child"
  run_moxy_mcp tools/call '{"name":"two.execute-command","arguments":{"cmd":"hello"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "executed: hello"' ||
    fail "conferred tool was not callable by the child: $output"
}

function show_effective_moxyfile_keeps_pass_tools_through_the_merge { # @test
  # The global file passes server two and allows one; the project file asks
  # for two at use, which a closer layer cannot lift, and passes restart, which
  # the global never held. Per tool the lowest level wins.
  cat >"$HOME/.config/moxy/moxyfile" <<'EOF'
allow-tools = ["one"]
pass-tools = ["two"]
EOF
  write_two_servers "$HOME/repo/moxyfile" 'allow-tools = ["one", "two.execute-command"]
pass-tools = ["restart"]'
  cd "$HOME/repo"
  run_moxy show-effective-moxyfile
  assert_success
  assert_output --partial 'allow-tools = ["one"]
pass-tools = ["two.execute-command"]'
  local rendered="$output"

  # The render is its own fixed point and narrows the directory it came from.
  printf '%s\n' "$rendered" >"$HOME/rendered.toml"
  run_moxy show-effective-moxyfile --dir "$HOME/rendered.toml"
  assert_equal "$output" "$rendered"
  run_moxy narrows --parent "$HOME/rendered.toml" --child "$HOME/repo"
  assert_success
  assert_output ""

  run_moxy_mcp tools/list
  assert_success
  assert_equal "$(tool_names)" '["one.execute-command"]'
}
