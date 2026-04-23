#! /usr/bin/env bats

# End-to-end tests for the streamable-http transport. Verifies session
# lifecycle (initialize, DELETE, re-init), SSE notification delivery on
# `restart`, and reconnect behavior after an SSE stream drop. See
# https://github.com/amarbel-llc/moxy/issues/178.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"

  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF
  cd "$HOME/repo"
}

teardown() {
  if [[ ${BATS_TEST_COMPLETED:-1} != "1" ]]; then
    dump_moxy_http_stderr
  fi
  sse_stop || true
  stop_moxy_http
  teardown_test_home
}

function handshake_and_healthz { # @test
  start_moxy_http
  run curl -sS -o /dev/null -w "%{http_code}" "$MOXY_HTTP_URL/healthz"
  assert_success
  assert_output "200"
}

function initialize_assigns_session_id { # @test
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  assert_equal "$HTTP_STATUS" "200"
  [[ -n $MOXY_SESSION_ID ]]

  local sid="$MOXY_SESSION_ID"
  http_post_mcp "tools/list" "" "$sid"
  assert_equal "$HTTP_STATUS" "200"
  echo "$output" | jq -e '.result.tools[] | select(.name == "srv.execute-command")'
}

function tools_list_without_session_returns_404 { # @test
  start_moxy_http

  http_post_mcp "tools/list"
  assert_equal "$HTTP_STATUS" "404"
}

function delete_terminates_session { # @test
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  http_delete_session "$sid"
  assert_equal "$HTTP_STATUS" "200"

  http_post_mcp "tools/list" "" "$sid"
  assert_equal "$HTTP_STATUS" "404"
}

function reinitialize_after_delete_yields_new_session_id { # @test
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid_before="$MOXY_SESSION_ID"

  http_delete_session "$sid_before"
  assert_equal "$HTTP_STATUS" "200"

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  assert_equal "$HTTP_STATUS" "200"
  local sid_after="$MOXY_SESSION_ID"

  [[ -n $sid_after ]]
  [[ $sid_before != "$sid_after" ]]
}

function reinitialize_without_delete_is_idempotent { # @test
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid_first="$MOXY_SESSION_ID"

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid_second="$MOXY_SESSION_ID"

  [[ -n $sid_first ]]
  [[ $sid_first == "$sid_second" ]]
}

function sse_receives_list_changed_after_restart { # @test
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  local sse_out
  sse_out=$(mktemp)
  sse_start "$sid" "$sse_out"

  http_post_mcp "tools/call" '{"name":"restart","arguments":{"server":"srv"}}' "$sid"
  assert_equal "$HTTP_STATUS" "200"
  echo "$output" | jq -e '.result.content[0].text | test("restarted successfully")'

  run sse_wait_for "$sse_out" "notifications/tools/list_changed" 5
  if [[ $status -ne 0 ]]; then
    echo "--- SSE stream contents ---" >&2
    cat "$sse_out" >&2
    dump_moxy_http_stderr
    rm -f "$sse_out"
    false
  fi

  sse_stop
  rm -f "$sse_out"
}

function sse_reconnect_delivers_new_notifications { # @test
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  # First SSE stream
  local sse_out_1
  sse_out_1=$(mktemp)
  sse_start "$sid" "$sse_out_1"
  sse_stop
  rm -f "$sse_out_1"

  # Second SSE stream with the same session id — proves reconnection
  local sse_out_2
  sse_out_2=$(mktemp)
  sse_start "$sid" "$sse_out_2"

  http_post_mcp "tools/call" '{"name":"restart","arguments":{"server":"srv"}}' "$sid"
  assert_equal "$HTTP_STATUS" "200"

  run sse_wait_for "$sse_out_2" "notifications/tools/list_changed" 5
  if [[ $status -ne 0 ]]; then
    echo "--- SSE stream 2 contents ---" >&2
    cat "$sse_out_2" >&2
    dump_moxy_http_stderr
    rm -f "$sse_out_2"
    false
  fi

  sse_stop
  rm -f "$sse_out_2"
}

function sse_gap_notifications_are_not_replayed { # @test
  # Documents the current "no resumability" behavior (see issue #168).
  # A notification emitted while no SSE stream is open is lost; the next
  # stream only sees notifications emitted after it reconnects.
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  # Fire a restart while NO SSE stream is open. The generated
  # list_changed notification has nowhere to go and is dropped.
  http_post_mcp "tools/call" '{"name":"restart","arguments":{"server":"srv"}}' "$sid"
  assert_equal "$HTTP_STATUS" "200"

  # Open a new SSE stream and wait briefly; the dropped notification
  # must NOT reappear.
  local sse_out
  sse_out=$(mktemp)
  sse_start "$sid" "$sse_out"

  run sse_wait_for "$sse_out" "notifications/tools/list_changed" 1
  if [[ $status -eq 0 ]]; then
    echo "--- SSE stream replayed a dropped notification ---" >&2
    cat "$sse_out" >&2
    dump_moxy_http_stderr
    sse_stop
    rm -f "$sse_out"
    false
  fi

  # Sanity: a restart fired AFTER the reopen does land on the stream.
  http_post_mcp "tools/call" '{"name":"restart","arguments":{"server":"srv"}}' "$sid"
  assert_equal "$HTTP_STATUS" "200"

  run sse_wait_for "$sse_out" "notifications/tools/list_changed" 5
  if [[ $status -ne 0 ]]; then
    echo "--- SSE stream did not receive post-reopen notification ---" >&2
    cat "$sse_out" >&2
    dump_moxy_http_stderr
    sse_stop
    rm -f "$sse_out"
    false
  fi

  sse_stop
  rm -f "$sse_out"
}
