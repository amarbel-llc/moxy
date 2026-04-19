#! /usr/bin/env bats
#
# Tests the gh-watch state-management scripts against the nix-built moxin
# bin/ (not through moxy), because exercising them through moxy would require
# real `gh` auth. A shadow `gh` stub on PATH returns canned JSON; the nix
# wrapper appends to PATH so the stub takes precedence.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output

  export MOXY_GH_WATCH_DIR="$HOME/gh-watch"
  export BIN="$BATS_TEST_DIRNAME/../result/share/moxy/moxins/get-hubbed/bin"

  # Shadow `gh` with a stub that returns canned JSON. Recognises:
  #   api repos/{o}/{r}/actions/runs/{id}
  #   api repos/{o}/{r}/actions/runs/{id}/jobs (with --jq)
  local stub_dir="$HOME/stub"
  mkdir -p "$stub_dir"
  cat >"$stub_dir/gh" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail

# Defaults — tests override via env.
status="${GH_STUB_STATUS:-in_progress}"
conclusion="${GH_STUB_CONCLUSION:-null}"
html_url="${GH_STUB_HTML_URL:-https://github.com/fake/run}"

# Compose a minimal run payload.
run_json=$(cat <<EOF
{"status":"$status","conclusion":$conclusion,"html_url":"$html_url","id":99}
EOF
)

jobs_json='{"jobs":[{"id":1,"name":"build","status":"completed","conclusion":"success"},{"id":2,"name":"test","status":"completed","conclusion":"failure"}]}'

# Parse out --jq filter if present.
jq_filter=""
args=()
while [ $# -gt 0 ]; do
  case "$1" in
    --jq) jq_filter="$2"; shift 2 ;;
    *)    args+=("$1"); shift ;;
  esac
done
set -- "${args[@]}"

# Dispatch on route.
case "${1:-} ${2:-}" in
  "api "*"/jobs")
    body="$jobs_json"
    ;;
  "api "*"/actions/runs/"*)
    body="$run_json"
    ;;
  *)
    echo "gh stub: unrecognised: $*" >&2
    exit 1
    ;;
esac

if [ -n "$jq_filter" ]; then
  printf '%s' "$body" | jq -r "$jq_filter"
else
  printf '%s' "$body"
fi
STUB
  chmod +x "$stub_dir/gh"
  export PATH="$stub_dir:$PATH"
}

teardown() {
  teardown_test_home
}

function watch_run_appends_target_line { # @test
  run "$BIN/watch-run" amarbel-llc/moxy 12345
  assert_success

  [ -f "$MOXY_GH_WATCH_DIR/targets" ]
  local line
  line=$(cat "$MOXY_GH_WATCH_DIR/targets")
  echo "$line" | grep -q '^run	amarbel-llc/moxy	12345	run:amarbel-llc/moxy#12345	'
}

function watch_run_is_idempotent { # @test
  "$BIN/watch-run" amarbel-llc/moxy 12345
  run "$BIN/watch-run" amarbel-llc/moxy 12345
  assert_success
  echo "$output" | grep -q "already watching"

  local n
  n=$(wc -l <"$MOXY_GH_WATCH_DIR/targets")
  [ "$n" = "1" ]
}

function watch_list_empty { # @test
  run "$BIN/watch-list"
  assert_success
  echo "$output" | grep -q "no gh-watch targets"
}

function watch_list_probes_live_state { # @test
  "$BIN/watch-run" amarbel-llc/moxy 12345
  GH_STUB_STATUS=in_progress GH_STUB_CONCLUSION=null \
    run "$BIN/watch-list"
  assert_success
  echo "$output" | grep -q 'run:amarbel-llc/moxy#12345'
  echo "$output" | grep -q 'in_progress'
}

function watch_remove_drops_target { # @test
  "$BIN/watch-run" amarbel-llc/moxy 12345
  "$BIN/watch-run" amarbel-llc/other 67890

  run "$BIN/watch-remove" "run:amarbel-llc/moxy#12345"
  assert_success
  echo "$output" | grep -q "removed:"

  ! grep -q '12345' "$MOXY_GH_WATCH_DIR/targets"
  grep -q '67890' "$MOXY_GH_WATCH_DIR/targets"
}

function watch_remove_missing_handle { # @test
  run "$BIN/watch-remove" "run:nonexistent#1"
  assert_success
  echo "$output" | grep -q "not watching:"
}

function ci_run_get_combines_run_and_jobs { # @test
  run "$BIN/ci-run-get" amarbel-llc/moxy 12345
  assert_success

  echo "$output" | jq -e '.run.status == "in_progress"'
  echo "$output" | jq -e '.jobs | length == 2'
  echo "$output" | jq -e '.jobs[0].name == "build"'
}

function ci_run_logs_requires_run_id { # @test
  run "$BIN/ci-run-logs" amarbel-llc/moxy
  [ "$status" != "0" ]
  echo "$output" | grep -q "usage:"
}
