#!/usr/bin/env bats

# bats file_tags=chix

# Tests for chix.develop-run (#425). develop-run must STREAM each step's
# stdout to its own stdout as it arrives, not hoard it via command
# substitution — moxy's async live output spool (FDR 0005) tees the
# moxin child's stdout as an io.MultiWriter, so a script that only prints
# at exit makes every long-running gate look wedged (spool_bytes stuck
# at 0). See moxin(7) RESULT SHAPING for the general streaming note.
#
# moxy#394: a command's own stderr is shown on success and failure, apart
# from nix's, and an argument holding newlines reaches the command whole.

load 'common'

BIN="${CHIX_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/chix/bin}"

setup() {
  setup_test_home

  NIX_STUB="$HOME/bin/nix"
  mkdir -p "$HOME/bin"
  export PATH="$HOME/bin:$PATH"
  # A stand-in for `nix develop <ref> -c <command...>`: nix's own chatter on
  # stderr, then the command itself. No shebang on the stub — the nix
  # sandbox lacks /usr/bin/env.
  write_nix_stub <<'EOF'
[ "$1" = develop ] || exit 99
shift 2
[ "$1" = -c ] || exit 98
shift
echo "warning: Git tree is dirty" >&2
exec "$@"
EOF
}

teardown() {
  teardown_test_home
}

write_nix_stub() {
  cat >"$NIX_STUB"
  chmod +x "$NIX_STUB"
}

# The first line lands in the output file well before the sleep elapses —
# a buffer-then-print-at-exit implementation would show nothing until the
# whole process exits.
function chix_develop_run_streams_stdout_as_it_arrives { # @test
  local out_file="$HOME/out.txt"
  "$BIN/develop-run" '[{"command":"bash","args":["-c","echo line-one; sleep 2; echo line-two"]}]' >"$out_file" 2>&1 &
  local pid=$!

  sse_wait_for "$out_file" "line-one" 2 || {
    echo "line-one never appeared while the command was still sleeping" >&2
    kill "$pid" 2>/dev/null || true
    return 1
  }

  # line-two is behind the 2s sleep — it must NOT have landed yet, proving
  # line-one arrived incrementally rather than both lines dumping at exit.
  run cat "$out_file"
  refute_output --partial "line-two"

  wait "$pid"
  run cat "$out_file"
  assert_output --partial "line-one"
  assert_output --partial "line-two"
}

function chix_develop_run_accumulates_multiple_steps_in_order { # @test
  run "$BIN/develop-run" '[{"command":"echo","args":["output-of-step-one"]},{"command":"echo","args":["output-of-step-two"]}]'
  assert_success
  assert_output --partial "output-of-step-one"
  assert_output --partial "output-of-step-two"
  # Preserve step ordering.
  local one_line two_line
  one_line=$(grep -n "output-of-step-one" <<<"$output" | head -1 | cut -d: -f1)
  two_line=$(grep -n "output-of-step-two" <<<"$output" | head -1 | cut -d: -f1)
  [ "$one_line" -lt "$two_line" ]
}

function chix_develop_run_stops_on_first_failure { # @test
  run "$BIN/develop-run" '[{"command":"bash","args":["-c","echo should-not-run-step-two-after-this; exit 1"]},{"command":"echo","args":["should-never-appear"]}]'
  assert_failure
  assert_output --partial "should-not-run-step-two-after-this"
  refute_output --partial "should-never-appear"
}

# Accepted behavior change (#425): on failure, stdout is streamed live as
# it happens, so already-emitted stdout appears BEFORE the failing step's
# stderr in the combined output.
function chix_develop_run_prints_stderr_after_streamed_stdout_on_failure { # @test
  run "$BIN/develop-run" '[{"command":"bash","args":["-c","echo partial output before failure; echo boom from stderr >&2; exit 1"]}]'
  assert_failure
  assert_output --partial "partial output before failure"
  assert_output --partial "boom from stderr"

  local stdout_line stderr_line
  stdout_line=$(grep -n "partial output before failure" <<<"$output" | head -1 | cut -d: -f1)
  stderr_line=$(grep -n "boom from stderr" <<<"$output" | head -1 | cut -d: -f1)
  [ "$stdout_line" -lt "$stderr_line" ]
}

function chix_develop_run_shows_the_commands_stderr_on_success_apart_from_nix { # @test
  run "$BIN/develop-run" '[{"command":"bash","args":["-c","echo the-output; echo a-diagnostic >&2"]}]'
  assert_success
  assert_output --partial "the-output"
  assert_line "[stderr of bash]"
  assert_line "a-diagnostic"
  # nix's own warnings are not the command's output when it succeeds
  refute_output --partial "Git tree is dirty"
}

function chix_develop_run_passes_a_multi_line_argument_whole { # @test
  run "$BIN/develop-run" '[{"command":"bash","args":["-c","echo marker-one\necho argc=$# first=$1","zero","f1","f2"]}]'
  assert_success
  assert_line "marker-one"
  assert_line "argc=2 first=f1"
}

function chix_develop_run_labels_a_failure_of_nix_itself { # @test
  write_nix_stub <<'EOF'
echo "error: flake 'path:.' does not provide attribute 'devShells'" >&2
exit 1
EOF
  run "$BIN/develop-run" '[{"command":"echo","args":["never-runs"]}]'
  assert_failure
  assert_line "[nix develop]"
  assert_output --partial "does not provide attribute"
  refute_output --partial "never-runs"
}
