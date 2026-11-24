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
    if [[ -n ${NESTED_MOXY_OUT:-} && -s $NESTED_MOXY_OUT.stderr ]]; then
      echo "--- nested moxy stderr ---" >&2
      cat "$NESTED_MOXY_OUT.stderr" >&2
    fi
  fi
  if [[ -n ${NESTED_MOXY_OUT:-} && -s $NESTED_MOXY_OUT.pid ]]; then
    kill "$(cat "$NESTED_MOXY_OUT.pid")" 2>/dev/null || true
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

  # SIGTERM to moxy itself ($MOXY_HTTP_PID is moxy: spawn_moxy_http execs
  # it): a clean exit, and the socket is gone by the time it has exited.
  local rc=0
  kill -TERM "$MOXY_HTTP_PID"
  wait "$MOXY_HTTP_PID" || rc=$?
  assert_equal "$rc" "0"
  [[ ! -e "$RUNTIME_DIR/server.sock" ]] || fail "server.sock left behind after moxy exited on SIGTERM"
}

function unix_socket_healthz_says_starting_until_children_are_ready { # @test
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "slow"
command = ["bash", "$FIXTURES_DIR/slow-start-server.bash"]
EOF
  export CLOWN_PLUGIN_RUNTIME_DIR="$RUNTIME_DIR"
  _ensure_madder_default_store
  spawn_moxy_http

  run moxy_curl -sS -w "\n%{http_code}" "$MOXY_HTTP_URL/healthz"
  assert_success
  assert_output --partial '"status":"starting"'
  assert_output --partial "503"

  local code i
  for ((i = 0; i < 100; i++)); do
    code=$(moxy_curl -sS -o /dev/null -w "%{http_code}" "$MOXY_HTTP_URL/healthz" 2>/dev/null || true)
    [[ $code == 200 ]] && break
    sleep 0.1
  done
  assert_equal "$code" "200"
}

function a_failure_after_the_handshake_exits_nonzero_and_removes_the_socket { # @test
  # No madder store in this test's $HOME: Open fails after the unix
  # handshake has been printed.
  export CLOWN_PLUGIN_RUNTIME_DIR="$RUNTIME_DIR"
  spawn_moxy_http
  run head -n 1 "$MOXY_HTTP_STDOUT"
  assert_output "1|1|unix|$RUNTIME_DIR/server.sock|streamable-http"

  # Bounded: if startup ever stopped failing, moxy would serve on and an
  # unbounded wait would hang the lane instead of failing this test.
  local i
  for ((i = 0; i < 100; i++)); do
    kill -0 "$MOXY_HTTP_PID" 2>/dev/null || break
    sleep 0.1
  done
  if kill -0 "$MOXY_HTTP_PID" 2>/dev/null; then
    cat "$MOXY_HTTP_STDERR" >&2
    fail "moxy still running 10s after the handshake; expected its startup to fail"
  fi
  local rc=0
  wait "$MOXY_HTTP_PID" || rc=$?
  ((rc != 0)) || fail "moxy exited 0 although startup failed"
  run cat "$MOXY_HTTP_STDERR"
  assert_output --partial "madder default store"
  [[ ! -e "$RUNTIME_DIR/server.sock" ]] || fail "server.sock left behind after a failed startup"
}

function refuses_a_set_but_empty_variable { # @test
  export CLOWN_PLUGIN_RUNTIME_DIR=""
  run start_moxy_http
  assert_failure
  assert_output --partial "exited before handshake"
  assert_output --partial "CLOWN_PLUGIN_RUNTIME_DIR"
  assert_output --partial "set but empty"
}

function a_nested_serve_http_gets_no_runtime_dir_and_leaves_the_socket_alone { # @test
  export NESTED_MOXY_OUT="$BATS_TEST_TMPDIR/nested"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "srv"
command = ["bash", "$FIXTURES_DIR/nested-serve-http.bash"]
EOF
  export CLOWN_PLUGIN_RUNTIME_DIR="$RUNTIME_DIR"
  start_moxy_http
  run stat -c %i "$RUNTIME_DIR/server.sock"
  local inode="$output"

  local i
  for ((i = 0; i < 100; i++)); do
    [[ -s $NESTED_MOXY_OUT.stdout ]] && break
    sleep 0.1
  done

  run cat "$NESTED_MOXY_OUT.env"
  assert_output "<unset>"
  run head -n 1 "$NESTED_MOXY_OUT.stdout"
  assert_output --regexp '^1\|1\|tcp\|127\.0\.0\.1:[0-9]+\|streamable-http$'

  # The parent's socket is the same file and still serves.
  run stat -c %i "$RUNTIME_DIR/server.sock"
  assert_output "$inode"
  http_post_mcp initialize '{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}'
  assert_equal "$HTTP_STATUS" "200"
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
