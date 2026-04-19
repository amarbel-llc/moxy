#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output

  # walkie-talkie requires SPINCLASS_SESSION_ID and uses WALKIE_TALKIE_LOG
  # to locate the bus log. Point both at test-owned values so tests don't
  # touch $XDG_STATE_HOME on the host.
  export SPINCLASS_SESSION_ID="test/session-a"
  export WALKIE_TALKIE_LOG="$HOME/walkie-talkie/bus.log"
}

teardown() {
  teardown_test_home
}

function send_writes_tab_separated_line { # @test
  local params
  params=$(jq -cn --arg n "walkie-talkie.send" \
    '{name: $n, arguments: {target: "test/session-b", body: "hello"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  [ -f "$WALKIE_TALKIE_LOG" ]

  local line
  line=$(cat "$WALKIE_TALKIE_LOG")
  echo "$line" | grep -q 'from=test/session-a'
  echo "$line" | grep -q 'to=test/session-b'
  echo "$line" | grep -q 'msg=hello'

  # Line has exactly 3 tab characters (ts, from, to, msg — 4 fields).
  local tabs
  tabs=$(echo -n "$line" | tr -cd '\t' | wc -c)
  [ "$tabs" = "3" ]
}

function send_rejects_body_with_newline { # @test
  local params
  params=$(jq -cn --arg n "walkie-talkie.send" \
    '{name: $n, arguments: {target: "all", body: "line1\nline2"}}')
  run_moxy_mcp_v1 "tools/call" "$params"

  # The tool call itself may succeed at the MCP layer but the underlying
  # command should exit nonzero and the log must remain empty.
  [ ! -s "$WALKIE_TALKIE_LOG" ] || [ ! -f "$WALKIE_TALKIE_LOG" ]
  echo "$output" | grep -qi "newline or tab"
}

function send_creates_parent_directory { # @test
  [ ! -d "$(dirname "$WALKIE_TALKIE_LOG")" ]

  local params
  params=$(jq -cn --arg n "walkie-talkie.send" \
    '{name: $n, arguments: {target: "all", body: "first"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  [ -d "$(dirname "$WALKIE_TALKIE_LOG")" ]
  [ -f "$WALKIE_TALKIE_LOG" ]
}

function backscroll_returns_last_n_lines { # @test
  mkdir -p "$(dirname "$WALKIE_TALKIE_LOG")"
  {
    printf '2026-04-19T10:00:00Z\tfrom=test/session-a\tto=all\tmsg=one\n'
    printf '2026-04-19T10:00:01Z\tfrom=test/session-a\tto=all\tmsg=two\n'
    printf '2026-04-19T10:00:02Z\tfrom=test/session-a\tto=all\tmsg=three\n'
  } >"$WALKIE_TALKIE_LOG"

  local params
  params=$(jq -cn --arg n "walkie-talkie.backscroll" \
    '{name: $n, arguments: {n: "2"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  # Output should contain the two most recent messages and NOT the first.
  echo "$output" | grep -q 'msg=two'
  echo "$output" | grep -q 'msg=three'
  ! echo "$output" | grep -q 'msg=one'
}

function backscroll_handles_missing_log { # @test
  [ ! -f "$WALKIE_TALKIE_LOG" ]

  local params
  params=$(jq -cn --arg n "walkie-talkie.backscroll" '{name: $n, arguments: {}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  echo "$output" | grep -q "no walkie-talkie log yet"
}
