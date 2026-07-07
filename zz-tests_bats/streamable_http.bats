#! /usr/bin/env bats

# bats file_tags=net_cap

# End-to-end tests for the streamable-http transport. Verifies session
# lifecycle (initialize, DELETE, re-init), SSE notification delivery on
# `restart`, and reconnect behavior after an SSE stream drop. See
# https://github.com/amarbel-llc/moxy/issues/178.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  # Do NOT `export output`: http_post_mcp stores the full tools/list response
  # (every tool's schema) in $output, and exporting it puts that large body
  # into the environment. Once envp crosses ARG_MAX, every later exec() in the
  # shell fails with E2BIG ("Argument list too long" — awk/rm), cascading into
  # a teardown temp-dir collision. $output is only read in-shell, so no export
  # is needed. See #284.
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

function listen_flag_binds_caller_addr_and_serves { # @test
  start_moxy_http_listen

  # The --listen path must NOT print the clown-plugin handshake line to
  # stdout — there is no plugin-host on the other end to consume it.
  run cat "$MOXY_HTTP_STDOUT"
  refute_output --partial "streamable-http"

  # /healthz and a full MCP initialize both work on the bound address.
  run curl -sS -o /dev/null -w "%{http_code}" "$MOXY_HTTP_URL/healthz"
  assert_success
  assert_output "200"

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  assert_equal "$HTTP_STATUS" "200"
  [[ -n $MOXY_SESSION_ID ]]
}

function no_listen_flag_still_prints_handshake { # @test
  # Backward-compat: the default (no --listen) path still binds an ephemeral
  # port and prints the clown-plugin-protocol handshake line on stdout.
  start_moxy_http
  run head -n 1 "$MOXY_HTTP_STDOUT"
  assert_output --partial "|streamable-http"
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

function expose_no_meta_drops_meta_tools_keeps_child { # @test
  # --expose no-meta hides moxy's control surface but keeps child tools.
  start_moxy_http --expose no-meta

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  http_post_mcp "tools/list" "" "$sid"
  assert_equal "$HTTP_STATUS" "200"
  local body="$output"
  run jq -e '.result.tools[] | select(.name == "srv.execute-command")' <<<"$body"
  assert_success
  run jq -e '.result.tools[] | select(.name == "restart")' <<<"$body"
  assert_failure

  # Hidden is also uncallable — the security boundary, not just a list filter.
  http_post_mcp "tools/call" '{"name":"restart","arguments":{"server":"srv"}}' "$sid"
  assert_equal "$HTTP_STATUS" "200"
  body="$output"
  run jq -e '.result.isError == true' <<<"$body"
  assert_success
  run jq -e '.result.content[0].text | test("not exposed")' <<<"$body"
  assert_success
}

function expose_resources_only_drops_all_tools { # @test
  # --expose resources-only advertises zero tools; resources (none here) would
  # still flow natively. The child's own tool is gone and uncallable.
  start_moxy_http --expose resources-only

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  http_post_mcp "tools/list" "" "$sid"
  assert_equal "$HTTP_STATUS" "200"
  run jq -r '.result.tools | length' <<<"$output"
  assert_output "0"

  http_post_mcp "tools/call" '{"name":"srv.execute-command","arguments":{"cmd":"echo hi"}}' "$sid"
  assert_equal "$HTTP_STATUS" "200"
  local body="$output"
  run jq -e '.result.isError == true' <<<"$body"
  assert_success
  run jq -e '.result.content[0].text | test("not exposed")' <<<"$body"
  assert_success
}

function expose_invalid_selector_refuses_to_start { # @test
  # A bad --expose selector fails fast: moxy exits before it ever serves.
  run start_moxy_http --expose bogus-profile
  assert_failure
}

function name_template_default_is_dotted { # @test
  # No --name-template flag: names keep the historical dot join (back-compat).
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  http_post_mcp "tools/list" "" "$sid"
  assert_equal "$HTTP_STATUS" "200"
  run jq -e '.result.tools[] | select(.name == "srv.execute-command")' <<<"$output"
  assert_success
}

function name_template_underscore_advertises_safe_names { # @test
  # --name-template '{server}_{tool}' renders dot-free names for strict
  # frontends (claude.ai rejects any dotted tool name). The dotted form is gone
  # and no advertised name contains a dot.
  start_moxy_http --name-template '{server}_{tool}'

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  http_post_mcp "tools/list" "" "$sid"
  assert_equal "$HTTP_STATUS" "200"
  local body="$output"
  run jq -e '.result.tools[] | select(.name == "srv_execute-command")' <<<"$body"
  assert_success
  run jq -e '.result.tools[] | select(.name == "srv.execute-command")' <<<"$body"
  assert_failure
  # The whole point: claude.ai's validator rejects any dot, so assert none.
  run jq -e '[.result.tools[].name | select(test("\\."))] | length == 0' <<<"$body"
  assert_success
}

function name_template_underscore_dispatch_round_trips { # @test
  # A call to the rendered name routes to the child's original tool name.
  start_moxy_http --name-template '{server}_{tool}'

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  http_post_mcp "tools/call" '{"name":"srv_execute-command","arguments":{"cmd":"echo hi"}}' "$sid"
  assert_equal "$HTTP_STATUS" "200"
  local body="$output"
  run jq -e '.result.isError != true' <<<"$body"
  assert_success
  run jq -e '.result.content[0].text | test("executed: echo hi")' <<<"$body"
  assert_success
}

function name_template_missing_tool_refuses_to_start { # @test
  # A template without {tool} is malformed — fail fast before serving.
  run start_moxy_http --name-template '{server}'
  assert_failure
}

function name_template_collision_refuses_to_start { # @test
  # Two servers both exposing 'execute-command' collide under '{tool}' (the
  # server prefix is dropped) → moxy refuses to serve rather than shadow one.
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]

[[servers]]
name = "srv2"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  run start_moxy_http --name-template '{tool}'
  assert_failure
}

function clown_system_prompt_fragment_served { # @test
  # clown fetches /clown/system-prompt once after health, before claude
  # launches (RFC-0002 §5). It returns a 200 Markdown fragment listing live
  # child-server state — here the connected `srv` fixture.
  start_moxy_http

  run curl -sS -o /dev/null -w "%{http_code}" "$MOXY_HTTP_URL/clown/system-prompt"
  assert_success
  assert_output "200"

  run curl -sS "$MOXY_HTTP_URL/clown/system-prompt"
  assert_success
  assert_output --partial "child servers"
  assert_output --partial "srv"
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
  echo "$output" | jq -e '.result.content[0].text | test("restarted successfully")' || fail ".result.content[0].text | test(\"restarted successfully\") check failed: $output"

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

function exclude_tools_hides_and_gates_excluded_tool { # @test
  # POST /clown/exclude-tools (FDR 0010, issue #399) is the dynamic runtime
  # counterpart to --expose: excluding "srv" wholesale drops srv.execute-command
  # from tools/list AND makes it uncallable, mirroring the --expose security
  # boundary above rather than being a cosmetic list filter.
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  http_post_mcp "tools/list" "" "$sid"
  assert_equal "$HTTP_STATUS" "200"
  run jq -e '.result.tools[] | select(.name == "srv.execute-command")' <<<"$output"
  assert_success

  run curl -sS -X POST -o /dev/null -w "%{http_code}" \
    -H "Content-Type: application/json" \
    --data '{"exclude":["srv"]}' \
    "$MOXY_HTTP_URL/clown/exclude-tools"
  assert_success
  assert_output "200"

  http_post_mcp "tools/list" "" "$sid"
  assert_equal "$HTTP_STATUS" "200"
  run jq -e '.result.tools[] | select(.name == "srv.execute-command")' <<<"$output"
  assert_failure

  http_post_mcp "tools/call" '{"name":"srv.execute-command","arguments":{"cmd":"echo hi"}}' "$sid"
  assert_equal "$HTTP_STATUS" "200"
  local body="$output"
  run jq -e '.result.isError == true' <<<"$body"
  assert_success
  run jq -e '.result.content[0].text | test("excluded")' <<<"$body"
  assert_success
}

function exclude_tools_get_reads_back_current_set { # @test
  start_moxy_http

  run curl -sS "$MOXY_HTTP_URL/clown/exclude-tools"
  assert_success
  run jq -e '.exclude | length == 0' <<<"$output"
  assert_success

  curl -sS -X POST -H "Content-Type: application/json" \
    --data '{"exclude":["srv.execute-command"]}' \
    "$MOXY_HTTP_URL/clown/exclude-tools" >/dev/null

  run curl -sS "$MOXY_HTTP_URL/clown/exclude-tools"
  assert_success
  run jq -e '.exclude == ["srv.execute-command"]' <<<"$output"
  assert_success
}

function exclude_tools_post_fully_replaces_prior_set { # @test
  # Full-replace semantics: a second POST with a different list must NOT
  # merge with the first — the prior exclusion is gone entirely.
  start_moxy_http

  curl -sS -X POST -H "Content-Type: application/json" \
    --data '{"exclude":["srv"]}' \
    "$MOXY_HTTP_URL/clown/exclude-tools" >/dev/null

  curl -sS -X POST -H "Content-Type: application/json" \
    --data '{"exclude":["restart"]}' \
    "$MOXY_HTTP_URL/clown/exclude-tools" >/dev/null

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  http_post_mcp "tools/list" "" "$sid"
  assert_equal "$HTTP_STATUS" "200"
  # srv.execute-command survived the replace; only "restart" stays excluded.
  run jq -e '.result.tools[] | select(.name == "srv.execute-command")' <<<"$output"
  assert_success
  run jq -e '.result.tools[] | select(.name == "restart")' <<<"$output"
  assert_failure
}

function exclude_tools_post_notifies_sse_list_changed { # @test
  # Mirrors the restart -> list_changed SSE tests above: POST
  # /clown/exclude-tools with a set that actually changes state must also
  # emit notifications/tools/list_changed on any open SSE stream.
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  local sid="$MOXY_SESSION_ID"

  local sse_out
  sse_out=$(mktemp)
  sse_start "$sid" "$sse_out"

  run curl -sS -X POST -o /dev/null -w "%{http_code}" \
    -H "Content-Type: application/json" \
    --data '{"exclude":["srv"]}' \
    "$MOXY_HTTP_URL/clown/exclude-tools"
  assert_success
  assert_output "200"

  run sse_wait_for "$sse_out" "notifications/tools/list_changed" 5
  if [[ $status -ne 0 ]]; then
    echo "--- SSE stream contents ---" >&2
    cat "$sse_out" >&2
    dump_moxy_http_stderr
    sse_stop
    rm -f "$sse_out"
    false
  fi

  sse_stop
  rm -f "$sse_out"
}
