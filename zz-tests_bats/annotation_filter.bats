#! /usr/bin/env bats

# bats file_tags=mcp_proto

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
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
  echo "$output" | jq -e '.tools[] | select(.name == "srv.list-items")' || fail ".tools[] | select(.name == \"srv.list-items\") check failed: $output"

  # update-item has idempotentHint=true — should match via OR.
  echo "$output" | jq -e '.tools[] | select(.name == "srv.update-item")' || fail ".tools[] | select(.name == \"srv.update-item\") check failed: $output"

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
  echo "$output" | jq -e '.tools[] | select(.name == "srv.list-items")' || fail ".tools[] | select(.name == \"srv.list-items\") check failed: $output"

  # update-item has readOnlyHint=false — no match.
  local update_count
  update_count=$(echo "$output" | jq '[.tools[] | select(.name == "srv.update-item")] | length')
  [[ $update_count -eq 0 ]]

  # delete-item has no annotations — no match.
  local delete_count
  delete_count=$(echo "$output" | jq '[.tools[] | select(.name == "srv.delete-item")] | length')
  [[ $delete_count -eq 0 ]]
}

# moxy#450: a tool the filter hides from tools/list is not callable by name
# either. The filter is a boundary, like --expose and allow-tools.

function annotation_filter_refuses_a_call_to_a_hidden_tool { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/annotated-tool-server.bash"]

[servers.annotations]
readOnlyHint = true
EOF
  cd "$HOME/repo"

  # the listed tool runs
  run_moxy_mcp tools/call '{"name":"srv.list-items"}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "items: a, b, c"' || fail "listed tool did not run: $output"

  # a hidden tool (readOnlyHint=false) and a tool with no annotations are
  # refused, and the child never runs them
  local tool
  for tool in srv.update-item srv.delete-item; do
    run_moxy_mcp tools/call "$(jq -cn --arg n "$tool" '{name: $n}')"
    assert_success
    echo "$output" | jq -e '.content[0].text | contains("hidden by its [servers.annotations] filter")' || fail "$tool was not refused: $output"
    echo "$output" | jq -e '.content[0].text | (contains("updated") or contains("deleted")) | not' || fail "$tool ran: $output"
  done
}

function annotation_filter_unset_leaves_every_tool_callable { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/annotated-tool-server.bash"]
EOF
  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"srv.update-item"}'
  assert_success
  echo "$output" | jq -e '.content[0].text == "updated"' || fail "$output"
}
