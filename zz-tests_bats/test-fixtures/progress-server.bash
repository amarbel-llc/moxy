#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server that emits notifications/progress during a tool call.

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
  initialize)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"progress-test","version":"0.1"}}}'
    ;;
  notifications/initialized) ;;
  tools/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"tools":[{"name":"slow-task","description":"A task that reports progress","inputSchema":{"type":"object"}}]}}'
    ;;
  tools/call)
    # Emit a progress notification before the result
    echo '{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"tok-1","progress":50,"total":100,"message":"halfway there"}}'
    # Then return the result
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"done"}]}}'
    ;;
  esac
done
