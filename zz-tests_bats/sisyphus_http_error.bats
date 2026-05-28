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

  # Write a minimal Python HTTP server with route-specific responses:
  #   GET /rest/api/3/myself        → 200 with a stub accountId (for @me)
  #   everything else               → 400 with Jira-shaped JSON error body
  MOCK_SCRIPT=$(mktemp --suffix=.py)
  cat >"$MOCK_SCRIPT" <<'PYEOF'
import http.server, json, sys

MYSELF = json.dumps({"accountId": "stub-account-id", "displayName": "Test User"}).encode()
ERROR_BODY = json.dumps({"errorMessages": ["issuetype is required"], "errors": {}}).encode()

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def _read_body(self):
        length = int(self.headers.get("Content-Length", 0))
        return self.rfile.read(length) if length else b""
    def do_GET(self):
        if self.path.rstrip("/") in ("/rest/api/3/myself", "/rest/api/2/myself"):
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(MYSELF)))
            self.end_headers()
            self.wfile.write(MYSELF)
        else:
            self._error()
    def do_POST(self):
        self._read_body()
        self._error()
    def do_PUT(self):
        self._read_body()
        self._error()
    def _error(self):
        self.send_response(400)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(ERROR_BODY)))
        self.end_headers()
        self.wfile.write(ERROR_BODY)

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

# ── #239: pipe-prose and diff-codeblock pass the ADF validator ─────────────
# Both inputs used to cause INVALID_INPUT from Jira v3 in older marklas.
# These tests confirm the validator does NOT raise for these constructs today:
# the error in the output must be the HTTP 400 from Jira (our mock), not a
# sisyphus-level validation error.

@test "create-issue: inline code with pipes passes validator (reaches Jira)" {
  # Inline code containing pipes — must not trigger the table validator.
  # arg-order: project summary description
  run "$BIN/create-issue" "PROJ" "Test" 'The node is `Foo|Bar|Baz`, reachable.'
  # Must fail at HTTP layer (400 from mock), not at the ADF validator.
  assert_failure
  assert_output --partial "400"
  refute_output --partial "description rejected"
}

@test "create-issue: diff codeblock passes validator (reaches Jira)" {
  # Fenced diff block — must not trigger codeBlock-with-marks validator.
  run "$BIN/create-issue" "PROJ" "Test" "$(printf '```diff\n-old();\n+new();\n```')"
  assert_failure
  assert_output --partial "400"
  refute_output --partial "description rejected"
}

# ── #238: update-issue accepts @me for assignee ────────────────────────────

@test "update-issue: @me assignee resolves via /myself and reaches Jira" {
  # The mock server returns a stub accountId for GET /rest/api/3/myself.
  # resolve_assignee(@me) → myself() → accountId=stub-account-id, then
  # update_issue_field PUT → 400 from our mock (past the assignee resolution).
  # This proves @me is accepted and resolved, not rejected as unsupported.
  run "$BIN/update-issue" "PROJ-1" "" "" "@me"
  assert_failure
  # Error must be HTTP 400 from Jira mock, not a resolve_assignee ValueError.
  assert_output --partial "400"
  refute_output --partial "not supported"
  refute_output --partial "did not match"
}
