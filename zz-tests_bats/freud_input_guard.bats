#! /usr/bin/env bats

# bats file_tags=freud

# moxy#495: freud's always-allow tools read only session transcripts under the
# projects directory, by a session UUID or an agent-id. A path, '..', a glob, a
# symlink out of the directory, a control character or an oversize id finds
# nothing and reads nothing.

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert

  export CLAUDE_PROJECTS_DIR="$BATS_TEST_TMPDIR/claude-projects"
  local proj_dir="$CLAUDE_PROJECTS_DIR/-test-project"
  mkdir -p "$proj_dir"

  SESSION_ID="aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
  AGENT_ID="agent-a1b2c3d4"
  echo '{"type":"user","message":{"content":"visible-in-session"}}' >"$proj_dir/$SESSION_ID.jsonl"
  mkdir -p "$proj_dir/$SESSION_ID/subagents"
  echo '{"type":"user","message":{"content":"visible-in-agent"}}' >"$proj_dir/$SESSION_ID/subagents/$AGENT_ID.jsonl"

  # a transcript-shaped file outside the projects directory, and ways to reach it
  mkdir -p "$BATS_TEST_TMPDIR/outside/subagents"
  echo '{"type":"user","message":{"content":"outside-secret"}}' >"$BATS_TEST_TMPDIR/outside/leak.jsonl"
  echo '{"type":"user","message":{"content":"outside-secret"}}' >"$BATS_TEST_TMPDIR/outside/subagents/$AGENT_ID.jsonl"
  ln -s "$BATS_TEST_TMPDIR/outside/leak.jsonl" "$proj_dir/bbbbbbbb-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl"
  ln -s "$BATS_TEST_TMPDIR/outside" "$proj_dir/cccccccc-bbbb-cccc-dddd-eeeeeeeeeeee"

  if [[ -n ${FREUD_BIN:-} ]]; then
    BIN="$(dirname "$FREUD_BIN")"
    PYTHON_PREFIX=()
  else
    BIN="$BATS_TEST_DIRNAME/../moxins/freud/bin"
    PYTHON_PREFIX=(python3)
  fi

  BAD_IDS=(
    "../outside/leak"
    "-test-project/../../outside/leak"
    "$BATS_TEST_TMPDIR/outside/leak"
    "/etc/passwd"
    "*"
    "????????-????-????-????-????????????"
    "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/../../../outside/leak"
    "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl"
    "agent-../../outside/leak"
    "agent-a1b2c3d4/../x"
    "..%2foutside%2fleak"
    "%2e%2e/outside/leak"
    "bbbbbbbb-bbbb-cccc-dddd-eeeeeeeeeeee"
    "cccccccc-bbbb-cccc-dddd-eeeeeeeeeeee"
    "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee "
    "-test-project"
    ""
  )
}

# Each tool that takes a session id.
ID_TOOLS="transcript messages summary subagents tool-usage"

run_tool() { # TOOL ID
  run "${PYTHON_PREFIX[@]}" "$BIN/$1" "$2"
}

function a_session_uuid_and_an_agent_id_still_resolve { # @test
  run_tool transcript "$SESSION_ID"
  assert_success
  assert_output --partial "visible-in-session"
  run_tool messages "$SESSION_ID"
  assert_success
  assert_output --partial "visible-in-session"
  run_tool messages "$AGENT_ID"
  assert_success
  assert_output --partial "visible-in-agent"
  # the symlinked project directory holds an agent transcript too; it is skipped
  refute_output --partial "outside-secret"
  run_tool summary "$SESSION_ID"
  assert_success
  run_tool subagents "$SESSION_ID"
  assert_success
  assert_output --partial "$AGENT_ID"
  run_tool tool-usage "$AGENT_ID"
  assert_success
}

function ids_that_are_paths_globs_or_escape_the_directory_read_nothing { # @test
  local t id
  for t in $ID_TOOLS; do
    for id in "${BAD_IDS[@]}"; do
      # tool-usage with no id aggregates recent sessions instead of reading one
      [[ -z $id && $t == tool-usage ]] && continue
      run_tool "$t" "$id"
      refute_output --partial "outside-secret"
      refute_output --partial "visible-in"
      refute_output --partial "root:"
      if [[ $t == subagents && -n $id ]]; then
        assert_output --partial "No subagents found"
      else
        assert_failure
      fi
    done
  done
}

function control_characters_and_oversize_ids_read_nothing { # @test
  local t id
  for t in $ID_TOOLS; do
    for id in $'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee\n../outside/leak' \
      $'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee\t' \
      "$(printf 'a%.0s' $(seq 5000))" \
      "agent-$(printf 'a%.0s' $(seq 200))"; do
      run_tool "$t" "$id"
      assert_failure
      refute_output --partial "outside-secret"
    done
  done
}

function a_nul_byte_cannot_reach_the_tools { # @test
  # bash cannot pass NUL in argv; the tool sees the id cut at the NUL
  run_tool transcript $'../outside/leak\0aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'
  assert_failure
  refute_output --partial "outside-secret"
}

function unique_tools_explicit_list_refuses_bad_ids { # @test
  run "${PYTHON_PREFIX[@]}" "$BIN/unique-tools" "" 7 "../outside/leak,bbbbbbbb-bbbb-cccc-dddd-eeeeeeeeeeee,/etc/passwd"
  assert_output --partial "No matching sessions found"
  refute_output --partial "outside-secret"
}
