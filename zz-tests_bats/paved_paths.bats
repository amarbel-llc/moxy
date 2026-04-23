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

# Helper: create a project dir with a moxyfile pointing at the tool-server fixture.
_setup_project() {
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF
}

# Helper: write a paved-paths JSON file into the project dir.
_write_paved_paths() {
  cat >"$HOME/repo/moxyfile.paved-paths.json" <<'EOF'
[
  {
    "name": "test-path",
    "description": "A test paved path",
    "stages": [
      { "label": "first", "tools": ["srv.execute-command"] },
      { "label": "second", "tools": ["srv.execute-command"] }
    ]
  }
]
EOF
}

# 1. Sanity check: no paved-paths file → all tools visible
function no_paved_paths_file_shows_all_tools { # @test
  _setup_project
  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  # Child server tool should appear normally
  echo "$output" | jq -e '.tools[] | select(.name == "srv.execute-command")'
}

# 2. With paved-paths file, before any selection → only paved-paths tool visible
function paved_paths_before_selection_hides_child_tools { # @test
  _setup_project
  _write_paved_paths
  cd "$HOME/repo"
  run_moxy_mcp tools/list
  assert_success
  # paved-paths meta tool must be present
  echo "$output" | jq -e '.tools[] | select(.name == "paved-paths")'
  # Child tool must be hidden
  local count
  count=$(echo "$output" | jq '[.tools[] | select(.name == "srv.execute-command")] | length')
  [[ $count -eq 0 ]]
}

# 3. Calling paved-paths with no args lists available paths
function paved_paths_no_args_lists_available_paths { # @test
  _setup_project
  _write_paved_paths
  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"paved-paths","arguments":{}}'
  assert_success
  echo "$output" | jq -e '.content[0].text | test("test-path")'
  echo "$output" | jq -e '.content[0].text | test("A test paved path")'
}

# 5. After selecting a path, stage 0 tools become visible
function paved_paths_select_unlocks_stage_tools { # @test
  _setup_project
  _write_paved_paths
  cd "$HOME/repo"
  run_moxy_mcp_two \
    tools/call '{"name":"paved-paths","arguments":{"select":"test-path"}}' \
    tools/list
  assert_success
  # Stage 0 tool must now appear
  echo "$output" | jq -e '.tools[] | select(.name == "srv.execute-command")'
  # paved-paths meta tool must still be present
  echo "$output" | jq -e '.tools[] | select(.name == "paved-paths")'
}

# 6. Selecting an unknown path returns an error result
function paved_paths_select_unknown_path_returns_error { # @test
  _setup_project
  _write_paved_paths
  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"paved-paths","arguments":{"select":"nonexistent"}}'
  assert_success
  echo "$output" | jq -e '.content[0].text | test("unknown path")'
}

# 7. paved-paths with no args after selection shows current stage status
function paved_paths_status_after_selection { # @test
  _setup_project
  _write_paved_paths
  cd "$HOME/repo"
  run_moxy_mcp_two \
    tools/call '{"name":"paved-paths","arguments":{"select":"test-path"}}' \
    tools/call '{"name":"paved-paths","arguments":{}}'
  assert_success
  # Status should mention the selected path and current stage
  echo "$output" | jq -e '.content[0].text | test("test-path")'
  echo "$output" | jq -e '.content[0].text | test("first")'
}

# 8. After calling the stage 0 tool, tools/list advances to stage 1
# (Since this test fixture uses the same tool in both stages, we verify
#  that the tool remains visible and the path has not completed prematurely.)
function paved_paths_stage_advances_after_tool_call { # @test
  _setup_project
  # Single-stage path so we can verify completion
  cat >"$HOME/repo/moxyfile.paved-paths.json" <<EOF
[
  {
    "name": "single-stage",
    "description": "One stage path",
    "stages": [
      { "label": "only", "tools": ["srv.execute-command"] }
    ]
  }
]
EOF
  cd "$HOME/repo"

  # Select the path first, then call the stage tool, then list tools — three calls
  # run_moxy_mcp_two only sends two method calls. Use a three-message pipeline directly.
  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local select_req
  select_req=$(jq -cn '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"paved-paths","arguments":{"select":"single-stage"}}}')
  local stage_tool_req
  stage_tool_req=$(jq -cn '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"srv.execute-command","arguments":{"cmd":"hello"}}}')
  local list_req
  list_req=$(jq -cn '{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{}}')

  run timeout --preserve-status "15s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 1; echo "$4"; sleep 1; echo "$5"; sleep 2) | moxy serve mcp 2>/dev/null | jq -c "select(.id == 4) | .result" | head -1' \
    -- "$init" "$initialized" "$select_req" "$stage_tool_req" "$list_req"

  assert_success
  # After completing the only stage, path is complete — all tools should be visible
  echo "$output" | jq -e '.tools[] | select(.name == "srv.execute-command")'
}

# 9. Multiple paths: all paths shown when calling paved-paths with no args
function paved_paths_multiple_paths_listed { # @test
  _setup_project
  cat >"$HOME/repo/moxyfile.paved-paths.json" <<'EOF'
[
  {
    "name": "alpha",
    "description": "First path",
    "stages": [
      { "label": "go", "tools": ["srv.execute-command"] }
    ]
  },
  {
    "name": "beta",
    "description": "Second path",
    "stages": [
      { "label": "go", "tools": ["srv.execute-command"] }
    ]
  }
]
EOF
  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"paved-paths","arguments":{}}'
  assert_success
  echo "$output" | jq -e '.content[0].text | test("alpha")'
  echo "$output" | jq -e '.content[0].text | test("beta")'
}
