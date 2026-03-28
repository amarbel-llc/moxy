bats_load_library bats-support
bats_load_library bats-assert
bats_load_library bats-assert-additions
bats_load_library bats-island
bats_load_library bats-emo

run_moxy() {
  run timeout --preserve-status "5s" moxy "$@"
}

# Send a JSON-RPC initialize handshake followed by a method call, capture the
# method's result as JSON in $output.
run_moxy_mcp() {
  local method="$1"
  shift
  local params="${1:-}"

  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local method_req
  if [[ -n $params ]]; then
    method_req=$(jq -cn --arg m "$method" --argjson p "$params" '{"jsonrpc":"2.0","id":2,"method":$m,"params":$p}')
  else
    method_req=$(jq -cn --arg m "$method" '{"jsonrpc":"2.0","id":2,"method":$m}')
  fi

  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | moxy 2>/dev/null | jq -c "select(.id == 2) | .result" | head -1' \
    -- "$init" "$initialized" "$method_req"
}

# Send two method calls in one session, capture the second result in $output.
# Usage: run_moxy_mcp_two method1 params1 method2 [params2]
run_moxy_mcp_two() {
  local method1="$1" params1="$2" method2="$3"
  local params2="${4:-}"

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

  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 1; echo "$4"; sleep 2) | moxy 2>/dev/null | jq -c "select(.id == 3) | .result" | head -1' \
    -- "$init" "$initialized" "$req1" "$req2"
}

# Like run_moxy but captures stderr separately for checking log messages.
run_moxy_mcp_with_stderr() {
  local method="$1"
  shift
  local params="${1:-}"

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

  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | moxy 2>"$4" | jq -c "select(.id == 2) | .result" | head -1' \
    -- "$init" "$initialized" "$method_req" "$stderr_file"

  MOXY_STDERR=$(cat "$stderr_file")
  rm -f "$stderr_file"
}
