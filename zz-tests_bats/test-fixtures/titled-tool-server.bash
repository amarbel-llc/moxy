#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server with title and annotations on tools.

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
  initialize)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"titled-test","version":"0.1"}}}'
    ;;
  notifications/initialized) ;;
  tools/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"tools":[{"name":"update_thing","title":"Update Thing","description":"Updates a thing","inputSchema":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]},"annotations":{"title":"Update Thing","readOnlyHint":false}}]}}'
    ;;
  tools/call)
    name=$(echo "$line" | jq -r '.params.name')
    case "$name" in
    update_thing)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"updated"}]}}'
      ;;
    *)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"unknown tool: '"$name"'"}],"isError":true}}'
      ;;
    esac
    ;;
  esac
done
