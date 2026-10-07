#! /usr/bin/env bats

# bats file_tags=net_cap

# MOXY_PARENT_MOXYFILE enforcement on the streamable-http transport: a
# narrowing moxyfile serves, a widening one never binds. The stdio cases and
# the verbs are in narrowing.bats.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"
  export MOXIN_PATH="$BATS_TEST_TMPDIR/no-moxins"
  mkdir -p "$MOXIN_PATH" "$HOME/parent" "$HOME/child"

  cat >"$HOME/parent/moxyfile" <<EOF
builtin-native = false

[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF
  export MOXY_PARENT_MOXYFILE="$HOME/parent/moxyfile"
  cd "$HOME/child"
}

teardown() {
  if [[ ${BATS_TEST_COMPLETED:-1} != "1" ]]; then
    dump_moxy_http_stderr
  fi
  stop_moxy_http
  teardown_test_home
}

function serve_http_starts_when_its_moxyfile_narrows_the_parent { # @test
  cp "$HOME/parent/moxyfile" "$HOME/child/moxyfile"
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  assert_equal "$HTTP_STATUS" "200"
  http_post_mcp tools/list "" "$MOXY_SESSION_ID"
  assert_equal "$HTTP_STATUS" "200"
  echo "$output" | jq -e '.result.tools[] | select(.name == "srv.execute-command")' >/dev/null ||
    fail "srv tool missing"
}

function serve_http_refuses_to_start_when_its_moxyfile_widens_the_parent { # @test
  cat >"$HOME/child/moxyfile" <<EOF
builtin-native = false

[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]

[[servers]]
name = "extra"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF

  # start_moxy_http reports the server's stderr when it exits before the
  # handshake, which is exactly the refusal.
  run start_moxy_http
  assert_failure
  assert_output --partial "exited before handshake"
  assert_output --partial "does not narrow MOXY_PARENT_MOXYFILE"
  assert_output --partial "servers.extra: not in parent"
  refute_output --partial "serving streamable-http"
}
