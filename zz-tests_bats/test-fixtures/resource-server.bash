#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server that advertises resource capabilities.
# Returns a JSON array of 10 items for resources/read.

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
  initialize)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"resources":{}},"serverInfo":{"name":"resource-test","version":"0.1"}}}'
    ;;
  notifications/initialized) ;;
  resources/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"resources":[{"uri":"test://items","name":"items","mimeType":"application/json"}]}}'
    ;;
  resources/read)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"contents":[{"uri":"test://items","mimeType":"application/json","text":"[1,2,3,4,5,6,7,8,9,10]"}]}}'
    ;;
  esac
done
