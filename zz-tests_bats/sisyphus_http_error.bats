#!/usr/bin/env bats

# bats file_tags=sisyphus

# Tests that sisyphus bin scripts surface Jira's response body when the SDK
# raises HTTPError. Covers #243.
#
# Strategy: run a minimal Python HTTP server that always returns 400 with a
# JSON error body. Point JIRA_URL at it. Call the bin script directly.
# Assert the error output contains the body text.

load 'common'

BIN="${SISYPHUS_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/sisyphus/bin}"

MOCK_PORT=""
MOCK_PID=""
MOCK_SCRIPT=""

_start_mock_server() {
  # Pick a random ephemeral port to avoid collisions across parallel test runs.
  MOCK_PORT=$(python3 -c "import socket; s=socket.socket(); s.bind(('',0)); print(s.getsockname()[1]); s.close()")

  # Write a one-shot Python HTTP server that returns 400 with a Jira-shaped
  # JSON body for every request.
  MOCK_SCRIPT=$(mktemp --suffix=.py)
  cat >"$MOCK_SCRIPT" <<'PYEOF'
import http.server, json, sys

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def _respond(self):
        body = json.dumps({"errorMessages": ["issuetype is required"], "errors": {}}).encode()
        self.send_response(400)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def do_GET(self): self._respond()
    def do_POST(self): self._respond()
    def do_PUT(self): self._respond()

port = int(sys.argv[1])
srv = http.server.HTTPServer(("127.0.0.1", port), Handler)
srv.serve_forever()
PYEOF
  python3 "$MOCK_SCRIPT" "$MOCK_PORT" &
  MOCK_PID=$!
  # Give the server a moment to bind.
  sleep 0.3
}

_stop_mock_server() {
  [[ -n ${MOCK_PID:-} ]] && kill "$MOCK_PID" 2>/dev/null || true
  [[ -n ${MOCK_SCRIPT:-} ]] && rm -f "$MOCK_SCRIPT"
}

setup() {
  setup_test_home
  _start_mock_server
  export JIRA_URL="http://127.0.0.1:$MOCK_PORT"
  export JIRA_USERNAME="test"
  export JIRA_API_TOKEN="test"
}

teardown() {
  _stop_mock_server
  teardown_test_home
}

@test "create-issue: includes Jira response body on 400" {
  # arg-order: project summary description issuetype ...
  run "$BIN/create-issue" "PROJ" "Test issue" "" "InvalidType"
  # The script exits non-zero and emits the error via _lib.emit.
  # The JSON output content text must contain the HTTP error details.
  assert_failure
  assert_output --partial "issuetype is required"
  assert_output --partial "400"
}

@test "update-issue: includes Jira response body on 400" {
  # arg-order: issue_key summary
  run "$BIN/update-issue" "PROJ-1" "Updated summary"
  assert_failure
  assert_output --partial "issuetype is required"
  assert_output --partial "400"
}

@test "comment: includes Jira response body on 400" {
  # arg-order: issue_key body
  run "$BIN/comment" "PROJ-1" "A comment body"
  assert_failure
  assert_output --partial "issuetype is required"
  assert_output --partial "400"
}
