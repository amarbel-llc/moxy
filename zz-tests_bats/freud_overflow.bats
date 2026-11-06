#! /usr/bin/env bats

# bats file_tags=freud

# moxy#139: a tool result too large to return inline is neither a success
# nor a failure. freud counts it in its own Ovfl column: moxy's cached
# result (its "⚠ TRUNCATED" banner) and Claude Code's own "exceeds maximum
# allowed tokens" refusal alike, so they inflate neither OK nor Err.

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert

  export CLAUDE_PROJECTS_DIR="$BATS_TEST_TMPDIR/claude-projects"
  local proj_dir="$CLAUDE_PROJECTS_DIR/-test-project"
  mkdir -p "$proj_dir"
  cat >"$proj_dir/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl" <<'JSONL'
{"type":"user","cwd":"/test/project","message":{"content":"hello"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"big.tool","input":{}},{"type":"tool_use","id":"t2","name":"big.tool","input":{}},{"type":"tool_use","id":"t3","name":"big.tool","input":{}},{"type":"tool_use","id":"t4","name":"big.tool","input":{}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"fine"}]},{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"exit 1: boom"},{"type":"tool_result","tool_use_id":"t3","content":[{"type":"text","text":"⚠ TRUNCATED — showing 20 of 300 lines. Full output below.\nFull output: madder://blobs/blake2b256-x\n"}]},{"type":"tool_result","tool_use_id":"t4","is_error":true,"content":"Error: result (90,000 characters) exceeds maximum allowed tokens. Output has been saved to /tmp/x.txt"}]}}
JSONL

  if [[ -n ${FREUD_BIN:-} ]]; then
    BIN="$(dirname "$FREUD_BIN")"
    PYTHON_PREFIX=()
  else
    BIN="$BATS_TEST_DIRNAME/../moxins/freud/bin"
    PYTHON_PREFIX=(python3)
  fi
}

function the_summary_counts_overflows_apart_from_ok_and_errors { # @test
  run "${PYTHON_PREFIX[@]}" "$BIN/tool-results-summary" "" "7" "" "30" "both" ""
  assert_success
  assert_output --partial "Total: 4 calls (1 ok, 1 errors, 0 empty, 0 rejected, 2 overflow)"
  # Total OK Err Empty Rej Ovfl
  assert_output --regexp 'big\.tool +4 +1 +1 +0 +0 +2 '
}

function the_timeseries_counts_overflows_apart_from_ok_and_errors { # @test
  run "${PYTHON_PREFIX[@]}" "$BIN/tool-results-timeseries" "" "7" "" "day" "both" ""
  assert_success
  assert_output --regexp 'TOTAL +4 +1 +1 +0 +0 +2 '
}
