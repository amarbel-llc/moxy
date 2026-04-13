#! /usr/bin/env bats

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert

  # Create a fake CLAUDE_PROJECTS_DIR with a fixture session
  export CLAUDE_PROJECTS_DIR="$BATS_TEST_TMPDIR/claude-projects"
  local proj_dir="$CLAUDE_PROJECTS_DIR/-test-project"
  mkdir -p "$proj_dir"

  SESSION_ID="aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

  # Build fixture JSONL with tool_use and tool_result blocks
  cat >"$proj_dir/$SESSION_ID.jsonl" <<'JSONL'
{"type":"user","message":{"content":"hello"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu-1","name":"greeter.hello","input":{"name":"world"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu-1","content":[{"type":"text","text":"Hello, world! Welcome to the system."}]}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"The greeting was sent."}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu-2","name":"broken.tool","input":{"query":"test"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu-2","is_error":true,"content":[{"type":"text","text":"Command failed: exit code 1"}]}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu-3","name":"greeter.goodbye","input":{}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu-3","content":[{"type":"text","text":"Goodbye!"}]}]}}
JSONL

  TOOL_USAGE="$BATS_TEST_DIRNAME/../moxins/freud/bin/tool-usage"
}

function default_output_shows_msg_index { # @test
  run python3 "$TOOL_USAGE" "$SESSION_ID"
  assert_success
  # Should show message index (msg:N) for cross-referencing with messages tool
  assert_output --partial "msg:"
  # Should show all 3 tool calls
  assert_output --partial "greeter.hello"
  assert_output --partial "broken.tool"
  assert_output --partial "greeter.goodbye"
  assert_output --partial "3 tool call(s)"
  # Should NOT show result text by default
  refute_output --partial "Hello, world!"
  refute_output --partial "Command failed"
}

function include_results_shows_result_text { # @test
  run python3 "$TOOL_USAGE" "$SESSION_ID" "" "0" "true"
  assert_success
  assert_output --partial "greeter.hello"
  # Result text should appear
  assert_output --partial "Hello, world!"
  assert_output --partial "Goodbye!"
}

function include_results_shows_error_prefix { # @test
  run python3 "$TOOL_USAGE" "$SESSION_ID" "" "0" "true"
  assert_success
  # Error results should be marked
  assert_output --partial "ERROR"
  assert_output --partial "Command failed"
}

function tool_name_filter_works_with_include_results { # @test
  run python3 "$TOOL_USAGE" "$SESSION_ID" "broken" "0" "true"
  assert_success
  assert_output --partial "broken.tool"
  assert_output --partial "ERROR"
  assert_output --partial "Command failed"
  # Should NOT show other tools
  refute_output --partial "greeter.hello"
  refute_output --partial "greeter.goodbye"
  assert_output --partial "1 tool call(s)"
}
