#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server with hyphenated tool names.

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
  initialize)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"tool-test","version":"0.1"}}}'
    ;;
  notifications/initialized) ;;
  tools/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"tools":[{"name":"execute-command","description":"Run a command","inputSchema":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}]}}'
    ;;
  tools/call)
    name=$(echo "$line" | jq -r '.params.name')
    case "$name" in
    execute-command)
      cmd=$(echo "$line" | jq -r '.params.arguments.cmd')
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"executed: '"$cmd"'"}]}}'
      ;;
    *)
      echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"unknown tool: '"$name"'"}],"isError":true}}'
      ;;
    esac
    ;;
  esac
done
