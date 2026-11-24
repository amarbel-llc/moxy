#! /usr/bin/env bats

# bats file_tags=net_cap

# `moxy serve-http` under CLOWN_PLUGIN_RUNTIME_DIR (clown's plugin listeners
# contract v1): with the variable set and no --listen, moxy listens only on
# <dir>/server.sock, prints a `unix` handshake, opens no TCP listener and
# removes the socket on exit; it refuses a directory others can reach. Without
# the variable the ephemeral 127.0.0.1 listener is unchanged.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"

  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/tool-server.bash"]
EOF
  cd "$HOME/repo"

  # Kept short: the socket path must fit in sun_path (108 bytes).
  RUNTIME_DIR=$(mktemp -d "${TMPDIR:-/tmp}/prd.XXXXXX")
  chmod 700 "$RUNTIME_DIR"
}

teardown() {
  if [[ ${BATS_TEST_COMPLETED:-1} != "1" ]]; then
    dump_moxy_http_stderr
  fi
  stop_moxy_http
  unset CLOWN_PLUGIN_RUNTIME_DIR
  rm -rf "$RUNTIME_DIR"
  teardown_test_home
}

function unix_socket_serves_mcp_and_opens_no_tcp_listener { # @test
  export CLOWN_PLUGIN_RUNTIME_DIR="$RUNTIME_DIR"
  start_moxy_http

  run head -n 1 "$MOXY_HTTP_STDOUT"
  assert_output "1|1|unix|$RUNTIME_DIR/server.sock|streamable-http"

  [[ -S "$RUNTIME_DIR/server.sock" ]] || fail "server.sock is not a socket"
  run stat -c %a "$RUNTIME_DIR/server.sock"
  assert_output "600"

  run moxy_curl -sS -o /dev/null -w "%{http_code}" "$MOXY_HTTP_URL/healthz"
  assert_success
  assert_output "200"

  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  assert_equal "$HTTP_STATUS" "200"
  [[ -n $MOXY_SESSION_ID ]] || fail "no Mcp-Session-Id over the socket"
  http_post_mcp tools/list "" "$MOXY_SESSION_ID"
  assert_equal "$HTTP_STATUS" "200"
  echo "$output" | jq -e '.result.tools[] | select(.name == "srv.execute-command")' >/dev/null ||
    fail "srv tool missing from tools/list over the socket"

  if pid_has_tcp_listener "$MOXY_HTTP_PID"; then
    fail "moxy opened a TCP listener although CLOWN_PLUGIN_RUNTIME_DIR was set"
  fi

  # $MOXY_HTTP_PID may be the `( cd && moxy )` subshell: killing it makes
  # moxy see its parent gone and shut down, a moment later. Wait for that.
  kill "$MOXY_HTTP_PID"
  wait "$MOXY_HTTP_PID" || true
  local i
  for ((i = 0; i < 100; i++)); do
    [[ -e "$RUNTIME_DIR/server.sock" ]] || break
    sleep 0.1
  done
  [[ ! -e "$RUNTIME_DIR/server.sock" ]] || fail "server.sock left behind 10s after exit"
}

function without_the_variable_the_tcp_listener_is_unchanged { # @test
  # Also the positive control for pid_has_tcp_listener: it must see this one.
  unset CLOWN_PLUGIN_RUNTIME_DIR
  start_moxy_http
  run head -n 1 "$MOXY_HTTP_STDOUT"
  assert_output --regexp '^1\|1\|tcp\|127\.0\.0\.1:[0-9]+\|streamable-http$'
  pid_has_tcp_listener "$MOXY_HTTP_PID" || fail "probe found no TCP listener on the tcp path"
  [[ ! -e "$RUNTIME_DIR/server.sock" ]] || fail "server.sock created without the variable"
}

function listen_flag_wins_over_the_variable { # @test
  export CLOWN_PLUGIN_RUNTIME_DIR="$RUNTIME_DIR"
  start_moxy_http_listen
  run cat "$MOXY_HTTP_STDOUT"
  refute_output --partial "streamable-http"
  [[ ! -e "$RUNTIME_DIR/server.sock" ]] || fail "server.sock created despite --listen"
}

function refuses_a_group_or_world_accessible_dir { # @test
  chmod 755 "$RUNTIME_DIR"
  export CLOWN_PLUGIN_RUNTIME_DIR="$RUNTIME_DIR"
  run start_moxy_http
  assert_failure
  assert_output --partial "exited before handshake"
  assert_output --partial "CLOWN_PLUGIN_RUNTIME_DIR"
  assert_output --partial "group or world"
  [[ ! -e "$RUNTIME_DIR/server.sock" ]] || fail "server.sock created in a refused dir"
}

function refuses_a_missing_dir { # @test
  export CLOWN_PLUGIN_RUNTIME_DIR="$RUNTIME_DIR/absent"
  run start_moxy_http
  assert_failure
  assert_output --partial "exited before handshake"
  assert_output --partial "CLOWN_PLUGIN_RUNTIME_DIR"
  assert_output --partial "does not exist"
}
