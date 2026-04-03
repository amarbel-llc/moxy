#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server with both tools and resources.

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
  initialize)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{},"resources":{}},"serverInfo":{"name":"combo-test","version":"0.2.0"}}}'
    ;;
  notifications/initialized) ;;
  tools/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"tools":[{"name":"run","description":"Run something","inputSchema":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}},{"name":"status","description":"Get status","inputSchema":{"type":"object","properties":{}}}]}}'
    ;;
  resources/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"resources":[{"uri":"combo://info","name":"info","description":"Server info"}]}}'
    ;;
  resources/templates/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"resourceTemplates":[{"uriTemplate":"combo://item/{id}","name":"item","description":"Get item by ID"}]}}'
    ;;
  resources/read)
    uri=$(echo "$line" | jq -r '.params.uri')
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"contents":[{"uri":"'"$uri"'","mimeType":"text/plain","text":"hello from combo"}]}}'
    ;;
  tools/call)
    name=$(echo "$line" | jq -r '.params.name')
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"called: '"$name"'"}]}}'
    ;;
  esac
done
