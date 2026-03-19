#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server that advertises prompt capabilities.
# Responds to initialize, prompts/list, and prompts/get.

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
    initialize)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"prompts":{}},"serverInfo":{"name":"prompt-test","version":"0.1"}}}'
      ;;
    notifications/initialized)
      # no response needed
      ;;
    prompts/list)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"prompts":[{"name":"greet","description":"Generate a greeting","arguments":[{"name":"name","description":"Name to greet","required":true}]}]}}'
      ;;
    prompts/get)
      name=$(echo "$line" | jq -r '.params.name')
      arg_name=$(echo "$line" | jq -r '.params.arguments.name // "world"')
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"description":"A greeting","messages":[{"role":"user","content":{"type":"text","text":"Hello, '"$arg_name"'!"}}]}}'
      ;;
  esac
done
