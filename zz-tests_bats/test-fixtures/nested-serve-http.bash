#!/usr/bin/env bash
set -euo pipefail

# Child server for plugin_runtime_dir.bats. On its first start it records
# whether CLOWN_PLUGIN_RUNTIME_DIR reached it and starts a nested
# `moxy serve-http` with no --listen, as a tool run by a session would; then
# it serves as the plain tool server. The nested moxy loads the same
# moxyfile, so its own copy of this script (NESTED_MOXY_INNER=1) only serves.
#
# NESTED_MOXY_OUT is the path prefix for the records: .env, .stdout,
# .stderr, .pid.

if [[ -z ${NESTED_MOXY_INNER:-} ]]; then
  out="$NESTED_MOXY_OUT"
  printf '%s\n' "${CLOWN_PLUGIN_RUNTIME_DIR-<unset>}" >"$out.env"
  NESTED_MOXY_INNER=1 MOXY_HEARTBEAT_INTERVAL=0 "${MOXY_BIN:-moxy}" serve-http \
    >"$out.stdout" 2>"$out.stderr" </dev/null &
  echo $! >"$out.pid"
fi

exec bash "$(dirname "$0")/tool-server.bash"
