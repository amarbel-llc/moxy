#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server that is slow to start: it sleeps $SLOW_START_SECONDS
# (default 3) before answering `initialize`, standing in for a child whose
# startup dominates moxy's boot (serve-http readiness tests, and the
# bin/debug-serve-http-gap.mjs measurement).

delay="${SLOW_START_SECONDS:-3}"

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
  initialize)
    sleep "$delay"
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"slow-start","version":"0.1"}}}'
    ;;
  notifications/initialized) ;;
  tools/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"tools":[{"name":"ping-slow","description":"Always available once started","inputSchema":{"type":"object"}}]}}'
    ;;
  esac
done
