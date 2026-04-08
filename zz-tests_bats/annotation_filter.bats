#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"
}

teardown() {
  teardown_test_home
}

# Issue #29: annotation filters should use OR semantics, not AND.

function annotation_filter_readOnly_matches_tool_with_readOnly_only { # @test
  # The annotated-tool-server has 3 tools:
  #   list-items:   readOnlyHint=true
  #   update-item:  readOnlyHint=false, idempotentHint=true
  #   delete-item:  no annotations
  #
  # With filter readOnlyHint=true + idempotentHint=true (OR semantics),
  # list-items should match (has readOnlyHint=true) and
  # update-item should match (has idempotentHint=true).
  # delete-item should NOT match (no annotations).
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/annotated-tool-server.bash"]

[servers.annotations]
readOnlyHint = true
idempotentHint = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success

  # list-items has readOnlyHint=true — should match via OR.
  echo "$output" | jq -e '.tools[] | select(.name == "srv.list-items")'

  # update-item has idempotentHint=true — should match via OR.
  echo "$output" | jq -e '.tools[] | select(.name == "srv.update-item")'

  # delete-item has no annotations — should NOT match.
  local count
  count=$(echo "$output" | jq '[.tools[] | select(.name == "srv.delete-item")] | length')
  [[ $count -eq 0 ]]
}

function annotation_filter_single_hint_filters_correctly { # @test
  # With only readOnlyHint=true in the filter, only list-items should match.
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/annotated-tool-server.bash"]

[servers.annotations]
readOnlyHint = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success

  # list-items has readOnlyHint=true — match.
  echo "$output" | jq -e '.tools[] | select(.name == "srv.list-items")'

  # update-item has readOnlyHint=false — no match.
  local update_count
  update_count=$(echo "$output" | jq '[.tools[] | select(.name == "srv.update-item")] | length')
  [[ $update_count -eq 0 ]]

  # delete-item has no annotations — no match.
  local delete_count
  delete_count=$(echo "$output" | jq '[.tools[] | select(.name == "srv.delete-item")] | length')
  [[ $delete_count -eq 0 ]]
}

# Issue #30: exec-mcp should handle missing tools gracefully.

function exec_mcp_nonexistent_tool_returns_clear_error { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"exec-mcp","arguments":{"server":"srv","tool":"nonexistent-tool","arguments":{}}}'
  assert_success

  # Should return an isError result with a helpful message mentioning the tool
  # name, not crash with "child process exited unexpectedly".
  echo "$output" | jq -e '.isError == true'
  echo "$output" | jq -r '.content[0].text' | grep -qi "not found\|unknown tool\|nonexistent-tool"
}

function exec_mcp_reports_tool_count_on_missing_tool { # @test
  # When exec-mcp forwards a tool call that doesn't exist on the child,
  # the error message should include the number of registered tools so the
  # user can tell if annotation filtering dropped everything.
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]

[servers.annotations]
openWorldHint = true
EOF

  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"exec-mcp","arguments":{"server":"srv","tool":"execute-command","arguments":{"cmd":"hello"}}}'
  assert_success

  # exec-mcp should validate the tool exists on the child before forwarding.
  # The tool-server's execute-command has no annotations, so with
  # openWorldHint=true filter it is filtered out (0 tools registered).
  # The error should mention "not found" and the tool count.
  echo "$output" | jq -e '.isError == true'
  echo "$output" | jq -r '.content[0].text' | grep -qi "not found"
}
