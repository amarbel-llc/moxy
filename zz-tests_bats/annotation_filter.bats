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

