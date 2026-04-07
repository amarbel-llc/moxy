#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server with a space in the tool name.

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
  initialize)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"space-test","version":"0.1"}}}'
    ;;
  notifications/initialized) ;;
  tools/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"tools":[{"name":"my tool","description":"A tool with a space","inputSchema":{"type":"object","properties":{"arg":{"type":"string"}},"required":["arg"]}}]}}'
    ;;
  tools/call)
    name=$(echo "$line" | jq -r '.params.name')
    case "$name" in
    "my tool")
      arg=$(echo "$line" | jq -r '.params.arguments.arg')
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"got: '"$arg"'"}]}}'
      ;;
    *)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"unknown tool: '"$name"'"}],"isError":true}}'
      ;;
    esac
    ;;
  esac
done
