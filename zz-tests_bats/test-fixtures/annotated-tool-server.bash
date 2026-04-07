#!/usr/bin/env bash
set -euo pipefail

# MCP server with tools that have partial annotations.
# - "list-items": readOnlyHint=true only
# - "update-item": readOnlyHint=false, idempotentHint=true
# - "delete-item": no annotations at all

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
  initialize)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"annotated-test","version":"0.1"}}}'
    ;;
  notifications/initialized) ;;
  tools/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"tools":[{"name":"list-items","description":"List items","inputSchema":{"type":"object","properties":{}},"annotations":{"readOnlyHint":true}},{"name":"update-item","description":"Update an item","inputSchema":{"type":"object","properties":{"id":{"type":"string"}}},"annotations":{"readOnlyHint":false,"idempotentHint":true}},{"name":"delete-item","description":"Delete an item","inputSchema":{"type":"object","properties":{"id":{"type":"string"}}}}]}}'
    ;;
  tools/call)
    name=$(echo "$line" | jq -r '.params.name')
    case "$name" in
    list-items)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"items: a, b, c"}]}}'
      ;;
    update-item)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"updated"}]}}'
      ;;
    delete-item)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"deleted"}]}}'
      ;;
    *)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"unknown tool: '"$name"'"}],"isError":true}}'
      ;;
    esac
    ;;
  esac
done
