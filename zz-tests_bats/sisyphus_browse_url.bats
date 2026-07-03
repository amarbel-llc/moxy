#!/usr/bin/env bats

# bats file_tags=sisyphus

# #361: sisyphus tools surface the human-facing browse URL (<base>/browse/<KEY>)
# for created/fetched issues, derived from the configured JIRA_URL base. Uses a
# mock Jira server returning SUCCESS payloads (mirrors sisyphus_http_error.bats,
# which mocks the failure path).

load 'common'

BIN="${SISYPHUS_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/sisyphus/bin}"

MOCK_PORT=""
MOCK_PID=""
MOCK_SCRIPT=""

_start_mock_server() {
  MOCK_PORT=$(python3 -c "import socket; s=socket.socket(); s.bind(('',0)); print(s.getsockname()[1]); s.close()")

  MOCK_SCRIPT=$(mktemp --suffix=.py)
  cat >"$MOCK_SCRIPT" <<'PYEOF'
import http.server, json, sys

MYSELF = json.dumps({"accountId": "stub", "displayName": "Test User"}).encode()
CREATED = json.dumps({"key": "PROJ-1", "id": "10001", "self": "http://x/rest/api/3/issue/10001"}).encode()
ISSUE = json.dumps({
    "key": "PROJ-1",
    "fields": {
        "summary": "Test issue",
        "status": {"name": "To Do"},
        "issuetype": {"name": "Task"},
        "priority": {"name": "Medium"},
    },
}).encode()

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def _send(self, code, payload):
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
    def _read(self):
        n = int(self.headers.get("Content-Length", 0))
        return self.rfile.read(n) if n else b""
    def do_GET(self):
        p = self.path.split("?")[0].rstrip("/")
        if p.endswith("/myself"):
            self._send(200, MYSELF)
        else:
            self._send(200, ISSUE)
    def do_POST(self):
        self._read()
        self._send(200, CREATED)
    def do_PUT(self):
        self._read()
        self._send(204, b"")

port = int(sys.argv[1])
http.server.HTTPServer(("127.0.0.1", port), Handler).serve_forever()
PYEOF
  python3 "$MOCK_SCRIPT" "$MOCK_PORT" &
  MOCK_PID=$!
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

function create_issue_text_includes_browse_url { # @test
  run "$BIN/create-issue" "PROJ" "Test issue"
  assert_success
  assert_output --partial "http://127.0.0.1:$MOCK_PORT/browse/PROJ-1"
}

# arg-order: project summary description issuetype priority labels assignee parent output_format
function create_issue_json_includes_browse_url_field { # @test
  run "$BIN/create-issue" "PROJ" "Test issue" "" "" "" "" "" "" "json"
  assert_success
  assert_output --partial "browse_url"
  assert_output --partial "browse/PROJ-1"
}

function get_issue_text_includes_browse_url { # @test
  run "$BIN/get-issue" "PROJ-1"
  assert_success
  assert_output --partial "Browse: http://127.0.0.1:$MOCK_PORT/browse/PROJ-1"
}

# arg-order: issue_key fields output_format
function get_issue_json_includes_browse_url_field { # @test
  run "$BIN/get-issue" "PROJ-1" "" "json"
  assert_success
  assert_output --partial "browse_url"
}
