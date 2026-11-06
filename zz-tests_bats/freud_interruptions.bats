#! /usr/bin/env bats

# bats file_tags=freud

# moxy#136: every row of freud's interruptions table satisfies
# Approved + Rejected + Interrupted == Total, including tools that were
# rejected without ever being prompted (a hook deny, an always-allow tool).

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert

  export CLAUDE_PROJECTS_DIR="$BATS_TEST_TMPDIR/claude-projects"
  local proj_dir="$CLAUDE_PROJECTS_DIR/-test-project"
  mkdir -p "$proj_dir"
  SESSION_ID="aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

  # solo.tool: never prompted, rejected once, interrupted once.
  # asked.tool: prompted twice, rejected once (so approved once).
  cat >"$proj_dir/$SESSION_ID.jsonl" <<'JSONL'
{"type":"user","cwd":"/test/project","message":{"content":"hello"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu-1","name":"solo.tool","input":{}}]}}
{"type":"user","toolUseResult":"User rejected tool use","sourceToolUseID":"tu-1","message":{"content":[{"type":"tool_result","tool_use_id":"tu-1","content":"rejected"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu-2","name":"solo.tool","input":{}}]}}
{"type":"user","message":{"content":"[Request interrupted by user]"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu-3","name":"asked.tool","input":{}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu-3","content":"ok"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu-4","name":"asked.tool","input":{}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu-4","is_error":true,"content":"The user doesn't want to proceed with this tool use."}]}}
JSONL

  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/log"
  mkdir -p "$XDG_LOG_HOME/moxy/hooks"
  cat >"$XDG_LOG_HOME/moxy/hooks/$SESSION_ID.jsonl" <<'JSONL'
{"hook_event_name":"PermissionRequest","tool_name":"asked.tool"}
{"hook_event_name":"PermissionRequest","tool_name":"asked.tool"}
JSONL

  if [[ -n ${FREUD_BIN:-} ]]; then
    BIN="$(dirname "$FREUD_BIN")"
    PYTHON_PREFIX=()
  else
    BIN="$BATS_TEST_DIRNAME/../moxins/freud/bin"
    PYTHON_PREFIX=(python3)
  fi
}

# Columns: Prompted Approved Rejected Interrupted Total. The em dash an
# unprompted row shows for Prompted/Approved is — in the JSON result.

function an_unprompted_rejection_counts_toward_total { # @test
  run "${PYTHON_PREFIX[@]}" "$BIN/interruptions" "" "7" "20"
  assert_success
  assert_output --regexp 'solo\.tool \[auto\] +\\u2014 +\\u2014 +1 +1 +2\\n'
}

function a_prompted_rejection_is_not_counted_twice { # @test
  run "${PYTHON_PREFIX[@]}" "$BIN/interruptions" "" "7" "20"
  assert_success
  assert_output --regexp 'asked\.tool +2 +1 +1 +0 +2\\n'
}

function the_summary_counts_approvals_per_tool { # @test
  run "${PYTHON_PREFIX[@]}" "$BIN/interruptions" "" "7" "20"
  assert_success
  assert_output --partial 'Total: 2 prompted, 1 approved, 2 rejected, 1 interrupted'
}
