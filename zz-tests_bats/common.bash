bats_load_library bats-support
bats_load_library bats-assert
bats_load_library bats-assert-additions
bats_load_library bats-island
bats_load_library bats-emo

# When running inside the moxy devshell, flake.nix pins explicit nix store
# man page paths in MANEATER_TEST_MANPATH. Re-export it as MANPATH (with no
# trailing colon) so `manpath(1)` — which maneater's locateSource() calls —
# returns exactly those paths and nothing else. This is the only way to
# make man-page tests reproducible across hosts; otherwise we pick up
# whatever jq/coreutils the host's $MANPATH happens to expose, and the CI
# runner's Ubuntu man pages diverge from what's on a developer's machine.
if [[ -n ${MANEATER_TEST_MANPATH:-} ]]; then
  export MANPATH="$MANEATER_TEST_MANPATH"
fi

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
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | moxy serve mcp 2>/dev/null | jq -c "select(.id == 2) | .result" | head -1' \
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
    '(echo "$1"; echo "$2"; echo "$3"; sleep 1; echo "$4"; sleep 2) | moxy serve mcp 2>/dev/null | jq -c "select(.id == 3) | .result" | head -1' \
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
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | moxy serve mcp 2>"$4" | jq -c "select(.id == 2) | .result" | head -1' \
    -- "$init" "$initialized" "$method_req" "$stderr_file"

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

  run timeout --preserve-status "10s" bash -c '
    gate="$4"
    {
      echo "$1"
      echo "$2"
      # Block until the reader signals that the init response arrived.
      read -r < "$gate"
      echo "$3"
      sleep 2
    } | moxy serve mcp 2>/dev/null | while IFS= read -r line; do
      id=$(echo "$line" | jq -r ".id // empty")
      if [[ "$id" == "1" ]]; then
        echo ready > "$gate"
      elif [[ "$id" == "2" ]]; then
        echo "$line" | jq -c ".result"
      fi
    done
  ' -- "$init" "$initialized" "$method_req" "$gate"

  rm -f "$gate"
}

# Send a V1 JSON-RPC initialize handshake, capture the initialize result in
# $output. Uses V1 protocol to get instructions field.
run_moxy_mcp_init() {
  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'

  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; sleep 2) | moxy serve mcp 2>/dev/null | jq -c "select(.id == 1) | .result" | head -1' \
    -- "$init" "$initialized"
}

# Send a JSON-RPC initialize handshake followed by a method call to folio,
# capture the method's result as JSON in $output.
run_folio_mcp() {
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
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | folio serve mcp 2>/dev/null | jq -c "select(.id == 2) | .result" | head -1' \
    -- "$init" "$initialized" "$method_req"
}

# Send a JSON-RPC initialize handshake followed by a method call to maneater,
# capture the method's result as JSON in $output.
run_maneater_mcp() {
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
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | maneater serve mcp 2>/dev/null | jq -c "select(.id == 2) | .result" | head -1' \
    -- "$init" "$initialized" "$method_req"
}
