#!/usr/bin/env bash
set -euo pipefail

# MCP server whose tools carry `_meta` on tools/list.
# - "create-issue": `_meta` with a kind and keys moxy knows nothing about
# - "plain": no `_meta` at all

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
  initialize)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"meta-test","version":"0.1"}}}'
    ;;
  notifications/initialized) ;;
  tools/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"tools":[{"name":"create-issue","description":"Create an issue","inputSchema":{"type":"object"},"_meta":{"kind":"issue","vendor/extra":{"n":1,"tags":["a","b"]},"flag":true}},{"name":"plain","description":"No meta","inputSchema":{"type":"object"}}]}}'
    ;;
  tools/call)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"ok"}]}}'
    ;;
  esac
done
