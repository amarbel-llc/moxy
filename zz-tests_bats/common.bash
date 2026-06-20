bats_load_library bats-support
bats_load_library bats-assert
bats_load_library bats-assert-additions
bats_load_library bats-island
bats_load_library bats-emo

# Helpers below resolve `moxy` and `madder` via env-var fallbacks so the
# same suite runs both inside `pkgs.testers.batsLane` (which exports
# `MOXY_BIN`/`MADDER_BIN` as absolute store paths via the binaries map)
# and in the devshell (where `MOXY_BIN` may be unset and the binary is
# expected on PATH). See flake.nix's `mkBatsLane` and #249.
run_moxy() {
  _ensure_madder_default_store
  local moxy_cwd
  moxy_cwd=$(_moxy_spawn_dir)
  run timeout --preserve-status "5s" bash -c 'cd "$1"; shift; "${MOXY_BIN:-moxy}" "$@"' \
    -- "$moxy_cwd" "$@"
}

# Ensure a `.default` madder blob store exists at the per-test $HOME
# (set up by setup_test_home). Each bats test gets its own $HOME, so
# initializing the store there gives every test its own store — no
# cross-test sharing or init races even with `bats --jobs N`.
#
# Madder walks up from CWD looking for `.madder/`, so any cd inside
# $HOME (e.g. `cd "$HOME/repo"`, `cd "$HOME/project"`) still resolves
# the same store. Moxy aborts startup if the store is missing, so
# every helper that spawns `moxy serve mcp` calls this first.
_ensure_madder_default_store() {
  [[ -d ${HOME:-} ]] || return 0
  local store_dir="$HOME/.madder/local/share/blob_stores/default"
  [[ -d $store_dir ]] && return 0
  (cd "$HOME" && "${MADDER_BIN:-madder}" init .default >/dev/null 2>&1) || true
  return 0
}

# Resolve the directory to spawn moxy from. Prefers the caller's CWD
# when it's inside $HOME (so tests that cd into $HOME/repo for
# moxyfile lookups still work), else falls back to $HOME so madder's
# walk-up finds the per-test .default store there.
_moxy_spawn_dir() {
  if [[ -z ${HOME:-} ]]; then
    echo "$PWD"
    return
  fi
  case "$PWD" in
    "$HOME"|"$HOME"/*) echo "$PWD" ;;
    *) echo "$HOME" ;;
  esac
}

# Send a JSON-RPC initialize handshake followed by a method call, capture the
# method's result as JSON in $output.
run_moxy_mcp() {
  local method="$1"
  shift
  local params="${1:-}"

  _ensure_madder_default_store
  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local method_req
  if [[ -n $params ]]; then
    method_req=$(jq -cn --arg m "$method" --argjson p "$params" '{"jsonrpc":"2.0","id":2,"method":$m,"params":$p}')
  else
    method_req=$(jq -cn --arg m "$method" '{"jsonrpc":"2.0","id":2,"method":$m}')
  fi

  local moxy_cwd
  moxy_cwd=$(_moxy_spawn_dir)
  run timeout --preserve-status "10s" bash -c \
    'cd "$1"; (echo "$2"; echo "$3"; echo "$4"; sleep 2) | "${MOXY_BIN:-moxy}" serve mcp 2>/dev/null | jq -c "select(.id == 2) | .result" | head -1' \
    -- "$moxy_cwd" "$init" "$initialized" "$method_req"
}

# Send two method calls in one session, capture the second result in $output.
# Usage: run_moxy_mcp_two method1 params1 method2 [params2]
run_moxy_mcp_two() {
  local method1="$1" params1="$2" method2="$3"
  local params2="${4:-}"

  _ensure_madder_default_store
  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'

  local req1
  if [[ -n $params1 ]]; then
    req1=$(jq -cn --arg m "$method1" --argjson p "$params1" '{"jsonrpc":"2.0","id":2,"method":$m,"params":$p}')
  else
    req1=$(jq -cn --arg m "$method1" '{"jsonrpc":"2.0","id":2,"method":$m}')
  fi

  local req2
  if [[ -n $params2 ]]; then
    req2=$(jq -cn --arg m "$method2" --argjson p "$params2" '{"jsonrpc":"2.0","id":3,"method":$m,"params":$p}')
  else
    req2=$(jq -cn --arg m "$method2" '{"jsonrpc":"2.0","id":3,"method":$m}')
  fi

  local moxy_cwd
  moxy_cwd=$(_moxy_spawn_dir)
  run timeout --preserve-status "10s" bash -c \
    'cd "$1"; (echo "$2"; echo "$3"; echo "$4"; sleep 1; echo "$5"; sleep 2) | "${MOXY_BIN:-moxy}" serve mcp 2>/dev/null | jq -c "select(.id == 3) | .result" | head -1' \
    -- "$moxy_cwd" "$init" "$initialized" "$req1" "$req2"
}

# Like run_moxy but captures stderr separately for checking log messages.
run_moxy_mcp_with_stderr() {
  local method="$1"
  shift
  local params="${1:-}"

  _ensure_madder_default_store
  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local method_req
  if [[ -n $params ]]; then
    method_req=$(jq -cn --arg m "$method" --argjson p "$params" '{"jsonrpc":"2.0","id":2,"method":$m,"params":$p}')
  else
    method_req=$(jq -cn --arg m "$method" '{"jsonrpc":"2.0","id":2,"method":$m}')
  fi

  local stderr_file
  stderr_file=$(mktemp)

  local moxy_cwd
  moxy_cwd=$(_moxy_spawn_dir)
  run timeout --preserve-status "10s" bash -c \
    'cd "$1"; (echo "$2"; echo "$3"; echo "$4"; sleep 2) | "${MOXY_BIN:-moxy}" serve mcp 2>"$5" | jq -c "select(.id == 2) | .result" | head -1' \
    -- "$moxy_cwd" "$init" "$initialized" "$method_req" "$stderr_file"

  MOXY_STDERR=$(cat "$stderr_file")
  rm -f "$stderr_file"
}

# Like run_moxy_mcp but uses V1 protocol version (2025-11-25).
# Waits for the initialize response before sending the method request
# so the server has completed V1 negotiation.
run_moxy_mcp_v1() {
  local method="$1"
  shift
  local params="${1:-}"

  _ensure_madder_default_store
  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local method_req
  if [[ -n $params ]]; then
    method_req=$(jq -cn --arg m "$method" --argjson p "$params" '{"jsonrpc":"2.0","id":2,"method":$m,"params":$p}')
  else
    method_req=$(jq -cn --arg m "$method" '{"jsonrpc":"2.0","id":2,"method":$m}')
  fi

  local gate
  gate=$(mktemp -u)
  mkfifo "$gate"

  local moxy_cwd
  moxy_cwd=$(_moxy_spawn_dir)
  run timeout --preserve-status "10s" bash -c '
    cd "$5"
    gate="$4"
    {
      echo "$1"
      echo "$2"
      # Block until the reader signals that the init response arrived.
      read -r < "$gate"
      echo "$3"
      sleep 2
    } | "${MOXY_BIN:-moxy}" serve mcp 2>/dev/null | while IFS= read -r line; do
      id=$(echo "$line" | jq -r ".id // empty")
      if [[ "$id" == "1" ]]; then
        echo ready > "$gate"
      elif [[ "$id" == "2" ]]; then
        echo "$line" | jq -c ".result"
      fi
    done
  ' -- "$init" "$initialized" "$method_req" "$gate" "$moxy_cwd"

  rm -f "$gate"
}

# Send a V1 JSON-RPC initialize handshake, capture the initialize result in
# $output. Uses V1 protocol to get instructions field.
run_moxy_mcp_init() {
  _ensure_madder_default_store
  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'

  local moxy_cwd
  moxy_cwd=$(_moxy_spawn_dir)
  run timeout --preserve-status "10s" bash -c \
    'cd "$1"; (echo "$2"; echo "$3"; sleep 2) | "${MOXY_BIN:-moxy}" serve mcp 2>/dev/null | jq -c "select(.id == 1) | .result" | head -1' \
    -- "$moxy_cwd" "$init" "$initialized"
}

# --- Streamable HTTP helpers ----------------------------------------------
# Start `moxy serve-http` as a background process in the current directory's
# moxyfile. Parses the clown-plugin-protocol handshake line on stdout to
# discover the ephemeral port, then waits for /healthz to return 200.
#
# Exports: MOXY_HTTP_PID, MOXY_HTTP_URL, MOXY_HTTP_STDOUT, MOXY_HTTP_STDERR.
# Caller should invoke stop_moxy_http in teardown.
start_moxy_http() {
  _ensure_madder_default_store
  MOXY_HTTP_STDOUT=$(mktemp)
  MOXY_HTTP_STDERR=$(mktemp)

  local moxy_cwd
  moxy_cwd=$(_moxy_spawn_dir)
  # Bats tests assert on application/json POST responses. The default
  # MOXY_HEARTBEAT_INTERVAL=30s wraps every POST in text/event-stream,
  # which breaks unrelated tests that jq-decode the body. Tests that
  # exercise heartbeat behavior should override this explicitly.
  # Extra args ("$@") are appended to serve-http, e.g. --expose <selector>.
  (cd "$moxy_cwd" && MOXY_HEARTBEAT_INTERVAL=0 "${MOXY_BIN:-moxy}" serve-http "$@") >"$MOXY_HTTP_STDOUT" 2>"$MOXY_HTTP_STDERR" </dev/null &
  MOXY_HTTP_PID=$!

  local line addr i
  for ((i = 0; i < 100; i++)); do
    if [[ -s $MOXY_HTTP_STDOUT ]]; then
      line=$(head -n 1 "$MOXY_HTTP_STDOUT")
      [[ -n $line ]] && break
    fi
    if ! kill -0 "$MOXY_HTTP_PID" 2>/dev/null; then
      echo "moxy serve-http exited before handshake; stderr:" >&2
      cat "$MOXY_HTTP_STDERR" >&2
      return 1
    fi
    sleep 0.1
  done
  if [[ -z ${line:-} ]]; then
    echo "moxy serve-http handshake timeout; stderr:" >&2
    cat "$MOXY_HTTP_STDERR" >&2
    return 1
  fi

  addr=$(awk -F'|' '{print $4}' <<<"$line")
  if [[ -z $addr ]]; then
    echo "invalid handshake line: $line" >&2
    return 1
  fi
  MOXY_HTTP_URL="http://$addr"

  local code
  for ((i = 0; i < 30; i++)); do
    code=$(curl -sS -o /dev/null -w "%{http_code}" "$MOXY_HTTP_URL/healthz" 2>/dev/null || true)
    if [[ $code == 200 ]]; then
      return 0
    fi
    sleep 0.1
  done
  echo "healthz never became 200; stderr:" >&2
  cat "$MOXY_HTTP_STDERR" >&2
  return 1
}

# Start `moxy serve-http --listen 127.0.0.1:0` (the caller-chosen fixed-bind
# path) in the current directory's moxyfile. Passing :0 lets the kernel pick
# the port so the test never races on a hardcoded number, while still driving
# the --listen branch: moxy suppresses the clown-plugin handshake on stdout
# and logs the bound address to stderr. We parse that address from stderr,
# then wait for /healthz. Same exports and teardown contract as
# start_moxy_http, so stop_moxy_http cleans up either.
start_moxy_http_listen() {
  _ensure_madder_default_store
  MOXY_HTTP_STDOUT=$(mktemp)
  MOXY_HTTP_STDERR=$(mktemp)

  local moxy_cwd
  moxy_cwd=$(_moxy_spawn_dir)
  (cd "$moxy_cwd" && MOXY_HEARTBEAT_INTERVAL=0 "${MOXY_BIN:-moxy}" serve-http --listen 127.0.0.1:0) >"$MOXY_HTTP_STDOUT" 2>"$MOXY_HTTP_STDERR" </dev/null &
  MOXY_HTTP_PID=$!

  local addr i
  for ((i = 0; i < 100; i++)); do
    addr=$(sed -n 's/.*streamable-http on \([0-9.]*:[0-9]*\) (--listen).*/\1/p' "$MOXY_HTTP_STDERR" | head -n 1)
    [[ -n $addr ]] && break
    if ! kill -0 "$MOXY_HTTP_PID" 2>/dev/null; then
      echo "moxy serve-http --listen exited before binding; stderr:" >&2
      cat "$MOXY_HTTP_STDERR" >&2
      return 1
    fi
    sleep 0.1
  done
  if [[ -z ${addr:-} ]]; then
    echo "moxy serve-http --listen bind timeout; stderr:" >&2
    cat "$MOXY_HTTP_STDERR" >&2
    return 1
  fi
  MOXY_HTTP_URL="http://$addr"

  local code
  for ((i = 0; i < 30; i++)); do
    code=$(curl -sS -o /dev/null -w "%{http_code}" "$MOXY_HTTP_URL/healthz" 2>/dev/null || true)
    if [[ $code == 200 ]]; then
      return 0
    fi
    sleep 0.1
  done
  echo "healthz never became 200; stderr:" >&2
  cat "$MOXY_HTTP_STDERR" >&2
  return 1
}

stop_moxy_http() {
  if [[ -n ${MOXY_HTTP_PID:-} ]] && kill -0 "$MOXY_HTTP_PID" 2>/dev/null; then
    kill "$MOXY_HTTP_PID" 2>/dev/null || true
    wait "$MOXY_HTTP_PID" 2>/dev/null || true
  fi
  rm -f "${MOXY_HTTP_STDOUT:-}" "${MOXY_HTTP_STDERR:-}"
  unset MOXY_HTTP_PID MOXY_HTTP_STDOUT MOXY_HTTP_STDERR MOXY_HTTP_URL MOXY_SESSION_ID
}

# Dump captured moxy stderr on test failure. Call from teardown before
# stop_moxy_http so the user can see what the server logged.
dump_moxy_http_stderr() {
  if [[ -n ${MOXY_HTTP_STDERR:-} ]] && [[ -s ${MOXY_HTTP_STDERR:-} ]]; then
    echo "--- moxy serve-http stderr ---" >&2
    cat "$MOXY_HTTP_STDERR" >&2
    echo "--- end ---" >&2
  fi
}

# Send a JSON-RPC POST to /mcp. Populates:
#   $output          — response body (text)
#   $HTTP_STATUS     — HTTP status code
#   $MOXY_SESSION_ID — value of the Mcp-Session-Id response header if set
#                      (overwrites previous value; unset if absent)
#
# Usage: http_post_mcp <method> [params_json_string] [session_id]
http_post_mcp() {
  local method="$1"
  local params="${2:-}"
  local sid="${3:-}"

  local body
  if [[ -n $params ]]; then
    body=$(jq -cn --arg m "$method" --argjson p "$params" \
      '{jsonrpc:"2.0",id:1,method:$m,params:$p}')
  else
    body=$(jq -cn --arg m "$method" \
      '{jsonrpc:"2.0",id:1,method:$m}')
  fi

  local headers_file body_file
  headers_file=$(mktemp)
  body_file=$(mktemp)

  local curl_args=(
    -sS -X POST
    -H "Content-Type: application/json"
    -D "$headers_file"
    -o "$body_file"
    -w "%{http_code}"
    --data "$body"
  )
  if [[ -n $sid ]]; then
    curl_args+=(-H "Mcp-Session-Id: $sid")
  fi

  HTTP_STATUS=$(curl "${curl_args[@]}" "$MOXY_HTTP_URL/mcp" 2>/dev/null || echo "000")
  output=$(cat "$body_file")
  MOXY_SESSION_ID=$(awk -F': *' 'BEGIN{IGNORECASE=1} tolower($1)=="mcp-session-id"{gsub(/\r/,"",$2); print $2}' "$headers_file")
  rm -f "$headers_file" "$body_file"
}

# DELETE /mcp with a session id. Sets $HTTP_STATUS.
http_delete_session() {
  local sid="$1"
  HTTP_STATUS=$(curl -sS -X DELETE -o /dev/null -w "%{http_code}" \
    -H "Mcp-Session-Id: $sid" "$MOXY_HTTP_URL/mcp" 2>/dev/null || echo "000")
}

# Open a long-running GET SSE stream in the background, writing received
# bytes to $1. Sets $SSE_PID. Caller invokes sse_stop to terminate.
sse_start() {
  local sid="$1"
  local outfile="$2"
  : >"$outfile"
  curl -sS -N \
    -H "Accept: text/event-stream" \
    -H "Mcp-Session-Id: $sid" \
    "$MOXY_HTTP_URL/mcp" >"$outfile" 2>/dev/null &
  SSE_PID=$!
  # Give curl a moment to open the stream and register with the registry.
  sleep 0.3
}

sse_stop() {
  if [[ -n ${SSE_PID:-} ]] && kill -0 "$SSE_PID" 2>/dev/null; then
    kill "$SSE_PID" 2>/dev/null || true
    wait "$SSE_PID" 2>/dev/null || true
  fi
  unset SSE_PID
}

# Wait up to $timeout seconds for $outfile to contain $pattern. Returns 0 on
# match, 1 on timeout.
sse_wait_for() {
  local outfile="$1"
  local pattern="$2"
  local timeout="${3:-3}"
  local deadline=$((SECONDS + timeout))
  while ((SECONDS < deadline)); do
    if grep -q "$pattern" "$outfile" 2>/dev/null; then
      return 0
    fi
    sleep 0.05
  done
  return 1
}

# Build a three-branch stack against a local bare remote. Layout:
#   main → pr-a → pr-b → pr-c   (each branch tracks its parent, one commit each)
# Caller passes the enclosing tmp root (typically "$HOME" inside setup_test_home).
# Exports: STACK_REMOTE, STACK_WORK, STACK_BRANCH_A, STACK_BRANCH_B, STACK_BRANCH_C.
setup_stack_fixture() {
  local root="$1"
  STACK_REMOTE="$root/remote.git"
  STACK_WORK="$root/work"
  STACK_BRANCH_A="pr-a"
  STACK_BRANCH_B="pr-b"
  STACK_BRANCH_C="pr-c"
  git init -q --bare "$STACK_REMOTE"
  git init -q -b main "$STACK_WORK"
  (
    cd "$STACK_WORK"
    git config user.email t@t
    git config user.name t
    git config commit.gpgSign false
    git remote add origin "$STACK_REMOTE"
    git commit --allow-empty -m base -q
    git push -q -u origin main

    git checkout -q -b "$STACK_BRANCH_A"
    git commit --allow-empty -m a1 -q
    git push -q -u origin "$STACK_BRANCH_A"

    git checkout -q -b "$STACK_BRANCH_B"
    git commit --allow-empty -m b1 -q
    git push -q -u origin "$STACK_BRANCH_B"

    git checkout -q -b "$STACK_BRANCH_C"
    git commit --allow-empty -m c1 -q
    git push -q -u origin "$STACK_BRANCH_C"
  )
  export STACK_REMOTE STACK_WORK STACK_BRANCH_A STACK_BRANCH_B STACK_BRANCH_C
}
