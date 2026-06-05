#!/usr/bin/env bats

# bats file_tags=ci_watch

# Tests for get-hubbed.ci-watch: it must return a {status:"watching", job_id}
# envelope immediately (the detached poller runs in the background), and the
# poller must eventually call `clown job done <id> --state succeeded` once the
# run reaches a terminal state. The kill switch (CLOWN_DISABLE_JOB_WAKEUP=1)
# must short-circuit with status:"disabled" and never touch clown.

load 'common'

BIN="${GET_HUBBED_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/get-hubbed/bin}"

setup() {
  setup_test_home

  mkdir -p "$HOME/bin"
  # Keep ci-watch's logfile inside the per-test $HOME so nothing leaks into the
  # real ~/.local/state, and so the detached poller is fully hermetic.
  export XDG_STATE_HOME="$HOME/.local/state"

  # Poll fast and time out quickly so the background poller resolves within the
  # test's bounded wait instead of the 6h production default.
  export CI_WATCH_POLL_SECONDS=1
  export CI_WATCH_TIMEOUT_SECONDS=30

  # Record file the clown stub appends every invocation to.
  export CLOWN_RECORD="$HOME/clown-record"

  # gh stub: ci-watch only calls `gh api` from the poller (resolveRepo is
  # short-circuited by the explicit OWNER/NAME arg). Report the run as already
  # completed/success; answer the /jobs endpoint too for robustness.
  # Note: no shebang — the nix sandbox lacks /usr/bin/env, so bash falls back to
  # executing shebang-less scripts as shell scripts.
  cat > "$HOME/bin/gh" <<'EOF'
set -euo pipefail
endpoint="${2:-}"
case "$endpoint" in
  */jobs) echo '{"jobs":[]}' ;;
  *) echo '{"status":"completed","conclusion":"success"}' ;;
esac
EOF
  chmod +x "$HOME/bin/gh"

  # clown stub: record every argv line; print a fixed id for `job start` so the
  # parent's watching envelope and the poller's `job done` share it.
  cat > "$HOME/bin/clown" <<'EOF'
set -euo pipefail
printf '%s\n' "$*" >> "${CLOWN_RECORD:-/dev/null}"
if [ "${1:-}" = "job" ] && [ "${2:-}" = "start" ]; then
  echo "ci-job-1"
fi
EOF
  chmod +x "$HOME/bin/clown"

  # Locate clown explicitly via the contract var, and prepend the stub dir so
  # our gh stub shadows the nix-wrapped gh (suffix pathMode lets user PATH win).
  export CLOWN_BIN="$HOME/bin/clown"
  export PATH="$HOME/bin:$PATH"
}

teardown() {
  teardown_test_home
}

# Wait up to $2 seconds for $CLOWN_RECORD to contain $1. Returns 0 on match.
wait_for_record() {
  local pattern="$1"
  local timeout="${2:-10}"
  local deadline=$((SECONDS + timeout))
  while ((SECONDS < deadline)); do
    if grep -qF "$pattern" "$CLOWN_RECORD" 2>/dev/null; then
      return 0
    fi
    sleep 0.1
  done
  return 1
}

# ci-watch returns the watching envelope immediately, carrying the clown job id.
function ci_watch_returns_watching_envelope_with_job_id { # @test
  run "$BIN/ci-watch" "123" "amarbel-llc/moxy"
  assert_success
  assert_output --partial '"status":"watching"'
  assert_output --partial '"job_id":"ci-job-1"'
  assert_output --partial '"run_id":"123"'
}

# The detached poller eventually marks the job done with the mapped state.
function ci_watch_poller_calls_clown_job_done_succeeded { # @test
  run "$BIN/ci-watch" "123" "amarbel-llc/moxy"
  assert_success

  run wait_for_record "job done ci-job-1 --state succeeded" 15
  assert_success

  # The done call also carries a result-ref hint pointing back at ci-run-get.
  run cat "$CLOWN_RECORD"
  assert_output --partial "job start --source get-hubbed --label ci-123"
  assert_output --partial "--result-ref get-hubbed ci-run-get 123"
}

# Kill switch: no watching, no clown calls, status:"disabled".
function ci_watch_disabled_short_circuits_without_clown { # @test
  export CLOWN_DISABLE_JOB_WAKEUP=1
  run "$BIN/ci-watch" "123" "amarbel-llc/moxy"
  assert_success
  assert_output --partial '"status":"disabled"'

  # clown was never invoked, so the record file must not exist.
  assert [ ! -f "$CLOWN_RECORD" ]
}
