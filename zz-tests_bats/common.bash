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

  local init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}'
  local initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
  local method_req
  method_req=$(jq -cn --arg m "$method" '{"jsonrpc":"2.0","id":2,"method":$m}')

  run timeout --preserve-status "10s" bash -c \
    '(echo "$1"; echo "$2"; echo "$3"; sleep 2) | moxy 2>/dev/null | jq -c "select(.id == 2) | .result" | head -1' \
    -- "$init" "$initialized" "$method_req"
}
