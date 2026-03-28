#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
  FIXTURES_DIR="$(cd "$(dirname "$BATS_TEST_FILE")/test-fixtures" && pwd)"
}

teardown() {
  teardown_test_home
}

function progress_notification_forwarded_to_client { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/progress-server.bash"]
EOF

  cd "$HOME/repo"

  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local call='{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"srv-slow_task","arguments":{}}}'

  # Capture ALL output lines (not just the response) to see notifications
  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | moxy 2>/dev/null' \
    -- "$init" "$initialized" "$call"
  assert_success

  # Should contain the progress notification
  echo "$output" | jq -e 'select(.method == "notifications/progress")'
  # Should contain the progress params
  echo "$output" | jq -e 'select(.method == "notifications/progress") | .params.message == "halfway there"'
  echo "$output" | jq -e 'select(.method == "notifications/progress") | .params.progress == 50'
}

function progress_notification_contains_expected_fields { # @test
  mkdir -p "$HOME/repo"
  cat > "$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/progress-server.bash"]
EOF

  cd "$HOME/repo"

  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local call='{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"srv-slow_task","arguments":{}}}'

  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | moxy 2>/dev/null' \
    -- "$init" "$initialized" "$call"
  assert_success

  # Tool call result should also be present
  echo "$output" | jq -e 'select(.id == 2) | .result.content[0].text == "done"'
}
