#! /usr/bin/env bats

# bats file_tags=freud

# moxy#339: freud renders times in the host's local zone with the offset
# shown, never as bare UTC that reads like local time.

setup() {
  bats_load_library bats-support
  bats_load_library bats-assert

  export CLAUDE_PROJECTS_DIR="$BATS_TEST_TMPDIR/claude-projects"
  local proj_dir="$CLAUDE_PROJECTS_DIR/-test-project"
  mkdir -p "$proj_dir"
  SESSION_FILE="$proj_dir/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl"
  echo '{"type":"user","cwd":"/test/project","message":{"content":"hello"}}' >"$SESSION_FILE"
  # 1970-01-01 00:00 UTC: 1969-12-31 20:00 four hours west of UTC.
  touch -d @0 "$SESSION_FILE"
  # A POSIX TZ string needs no tzdata in the sandbox: XYZ, four hours west.
  export TZ=XYZ4

  if [[ -n ${FREUD_BIN:-} ]]; then
    BIN="$(dirname "$FREUD_BIN")"
    PYTHON_PREFIX=()
  else
    BIN="$BATS_TEST_DIRNAME/../moxins/freud/bin"
    PYTHON_PREFIX=(python3)
  fi
}

function sessions_last_activity_is_local_with_its_offset { # @test
  run "${PYTHON_PREFIX[@]}" "$BIN/sessions" ""
  assert_success
  assert_output --partial '1969-12-31 20:00 -0400'
  refute_output --partial '1970-01-01 00:00'
}
