#! /usr/bin/env bats

# bats file_tags=net_cap

# A tool's `kind` over the streamable-http transport (#451), at the protocol
# version clown's juggler speaks. The stdio cases are in tool_kind.bats.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"
  export MOXIN_PATH="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$MOXIN_PATH/ring" "$HOME/repo"
  cat >"$MOXIN_PATH/ring/_moxin.toml" <<'EOF'
schema = 1
name = "ring"
EOF
  cat >"$MOXIN_PATH/ring/list-issues.toml" <<'EOF'
schema = 3
description = "list issues"
command = "echo"
kind = "list"
EOF
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/meta-tool-server.bash"]
EOF
  cd "$HOME/repo"
}

teardown() {
  if [[ ${BATS_TEST_COMPLETED:-1} != "1" ]]; then
    dump_moxy_http_stderr
  fi
  stop_moxy_http
  teardown_test_home
}

function serve_http_lists_a_moxin_kind_and_a_child_meta { # @test
  start_moxy_http

  http_post_mcp initialize '{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  assert_equal "$HTTP_STATUS" "200"
  http_post_mcp tools/list "" "$MOXY_SESSION_ID"
  assert_equal "$HTTP_STATUS" "200"

  assert_equal \
    "$(echo "$output" | jq -c '.result.tools[] | select(.name == "ring.list-issues") | ._meta')" \
    '{"kind":"list"}'
  assert_equal \
    "$(echo "$output" | jq -cS '.result.tools[] | select(.name == "srv.create-issue") | ._meta')" \
    '{"flag":true,"kind":"issue","vendor/extra":{"n":1,"tags":["a","b"]}}'
}
