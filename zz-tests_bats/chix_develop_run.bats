#!/usr/bin/env bats

# bats file_tags=chix

# Tests for chix.develop-run (#425). develop-run must STREAM each step's
# stdout to its own stdout as it arrives, not hoard it via command
# substitution — moxy's async live output spool (FDR 0005) tees the
# moxin child's stdout as an io.MultiWriter, so a script that only prints
# at exit makes every long-running gate look wedged (spool_bytes stuck
# at 0). See moxin(7) RESULT SHAPING for the general streaming note.

load 'common'

BIN="${CHIX_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/chix/bin}"

setup() {
  setup_test_home

  NIX_STUB="$HOME/bin/nix"
  mkdir -p "$HOME/bin"
  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

# Writes stdin to the nix stub and marks it executable. No shebang on the
# stub itself — the nix sandbox lacks /usr/bin/env.
write_nix_stub() {
  cat >"$NIX_STUB"
  chmod +x "$NIX_STUB"
}

# Runs a stubbed `nix` that emits a line, sleeps, then emits a second line.
# If develop-run streamed correctly, the first line lands in the output
# file well before the sleep elapses — a buffer-then-print-at-exit
# implementation would show nothing until the whole process exits.
function chix_develop_run_streams_stdout_as_it_arrives { # @test
  write_nix_stub <<'EOF'
set -euo pipefail
echo "line-one"
sleep 2
echo "line-two"
EOF

  local out_file="$HOME/out.txt"
  "$BIN/develop-run" '[{"command":"anything"}]' >"$out_file" 2>&1 &
  local pid=$!

  sse_wait_for "$out_file" "line-one" 2 || {
    echo "line-one never appeared while nix stub was still sleeping" >&2
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
  write_nix_stub <<'EOF'
set -euo pipefail
case "$4" in
  step-one) echo "output-of-step-one" ;;
  step-two) echo "output-of-step-two" ;;
esac
EOF

  run "$BIN/develop-run" '[{"command":"step-one"},{"command":"step-two"}]'
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
  write_nix_stub <<'EOF'
set -euo pipefail
case "$4" in
  step-one)
    echo "should-not-run-step-two-after-this"
    exit 1
    ;;
  step-two) echo "should-never-appear" ;;
esac
EOF

  run "$BIN/develop-run" '[{"command":"step-one"},{"command":"step-two"}]'
  assert_failure
  assert_output --partial "should-not-run-step-two-after-this"
  refute_output --partial "should-never-appear"
}

# Accepted behavior change (#425): on failure, stdout is streamed live as
# it happens, so already-emitted stdout now appears BEFORE the failing
# step's stderr in the combined output — the reverse of the old
# buffer-then-print ordering.
function chix_develop_run_prints_stderr_after_streamed_stdout_on_failure { # @test
  write_nix_stub <<'EOF'
set -euo pipefail
echo "partial output before failure"
echo "boom from stderr" >&2
exit 1
EOF

  run "$BIN/develop-run" '[{"command":"anything"}]'
  assert_failure
  assert_output --partial "partial output before failure"
  assert_output --partial "boom from stderr"

  local stdout_line stderr_line
  stdout_line=$(grep -n "partial output before failure" <<<"$output" | head -1 | cut -d: -f1)
  stderr_line=$(grep -n "boom from stderr" <<<"$output" | head -1 | cut -d: -f1)
  [ "$stdout_line" -lt "$stderr_line" ]
}
