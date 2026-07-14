#!/usr/bin/env bash
set -euo pipefail

# Minimal MCP server whose tool returns a single large newline-delimited JSON
# result, to exercise moxy's child-stdout size ceiling (#275). The payload size
# is controlled by BIGLINE_BYTES (default ~2 MiB) so a test can probe both
# under-ceiling delivery and over-ceiling rejection. 2 MiB is above the old
# 64KB/1MB scanner wall that used to wedge the child, and below the 64 MiB
# default ceiling.

bytes="${BIGLINE_BYTES:-2097152}"
payload=$(head -c "$bytes" /dev/zero | tr '\0' A)

while IFS= read -r line; do
  id=$(echo "$line" | jq -r '.id // empty')
  method=$(echo "$line" | jq -r '.method // empty')

  case "$method" in
  initialize)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"bigline-test","version":"0.1"}}}'
    ;;
  notifications/initialized) ;;
  tools/list)
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"tools":[{"name":"big","description":"Return a large payload","inputSchema":{"type":"object","properties":{}}}]}}'
    ;;
  tools/call)
    # One line: the whole result, including the ~2 MiB text, terminated by the
    # single newline echo appends.
    echo '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"'"$payload"'"}]}}'
    ;;
  esac
done
