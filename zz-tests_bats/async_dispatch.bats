#!/usr/bin/env bats

# bats file_tags=async

# E2E for the async dispatch meta tools (FDR 0004): a real moxy session
# backgrounds a moxin tool call, the detached job writes its result to the
# user-level madder store (hermetic via XDG_DATA_HOME), and the clown stub
# records the producer contract (`job start` → `job done --state succeeded
# --message "<tool>: <summary> (madder <digest>)" --result-ref
# "moxy async-result <id>"`).

load 'common'

setup() {
  setup_test_home

  # Hermetic user-level madder store root: moxy-async lands under
  # $XDG_DATA_HOME/madder/blob_stores/. Provision it here the way
  # home-manager does in production — moxy itself never creates it
  # (FDR 0004). This must run from a CWD with no .madder ancestry and
  # BEFORE anything creates $HOME/.madder, or madder#227's walk-up
  # shadowing would land the store in the wrong scope.
  export XDG_DATA_HOME="$HOME/xdg-data"
  mkdir -p "$XDG_DATA_HOME"
  (cd "$XDG_DATA_HOME" && "${MADDER_BIN:-madder}" init moxy-async >/dev/null 2>&1)

  # Test moxin: one always-allow echo tool (async-eligible) and one
  # each-use tool (must be rejected by the allow-only preflight).
  local moxin="$HOME/moxins/testmoxin"
  mkdir -p "$moxin"
  cat > "$moxin/_moxin.toml" <<'EOF'
schema = 1
name = "testmoxin"
description = "async e2e fixture"
EOF
  cat > "$moxin/echo.toml" <<'EOF'
schema = 3
result-type = "text"
content-type = "text/plain"
perms-request = "always-allow"
description = "echo a fixed line"
command = "bash"
args = ["-c", "printf 'hello async'"]

[input]
type = "object"
EOF
  cat > "$moxin/asky.toml" <<'EOF'
schema = 3
perms-request = "each-use"
description = "ask-tier tool"
command = "bash"
args = ["-c", "true"]

[input]
type = "object"
EOF
  cat > "$moxin/noasync.toml" <<'EOF'
schema = 3
perms-request = "always-allow"
permit-async = false
description = "allow-tier tool that forbids backgrounding"
command = "bash"
args = ["-c", "true"]

[input]
type = "object"
EOF
  export MOXIN_PATH="$HOME/moxins"

  # clown stub: records argv; `job start` prints a fixed id.
  # NEEDS a real shebang: moxy (a Go process) execs CLOWN_BIN directly, so
  # the shebang-less trick used by bash-invoked stubs (ENOEXEC shell retry)
  # does not apply — a shebang-less script fails with exec format error and
  # moxy silently falls back to minting a local id. /bin/sh is the one path
  # the nix sandbox guarantees.
  mkdir -p "$HOME/bin"
  export CLOWN_RECORD="$HOME/clown-record"
  cat > "$HOME/bin/clown" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "${CLOWN_RECORD:-/dev/null}"
if [ "${1:-}" = job ] && [ "${2:-}" = start ]; then
  echo "testmoxin.echo-e2e00001"
fi
EOF
  chmod +x "$HOME/bin/clown"
  export CLOWN_BIN="$HOME/bin/clown"
}

teardown() {
  teardown_test_home
}

# Wait up to $2 seconds for $CLOWN_RECORD to contain $1. On timeout, print
# the record so the bats failure shows what WAS emitted.
wait_for_record() {
  local pattern="$1"
  local timeout="${2:-10}"
  local deadline=$((SECONDS + timeout))
  while ((SECONDS < deadline)); do
    if grep -qF "$pattern" "$CLOWN_RECORD" 2>/dev/null; then
      return 0
    fi
    sleep 0.1
  done
  echo "=== record after ${timeout}s without match: ==="
  cat "$CLOWN_RECORD" 2>/dev/null || echo "(no record file)"
  return 1
}

# async returns a running handle immediately; the detached job completes and
# the clown done line carries the summary, digest, and result-ref.
function async_dispatch_full_producer_contract { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"async","arguments":{"tool":"testmoxin.echo","args":{}}}'
  assert_success
  assert_output --partial '\"status\":\"running\"'
  assert_output --partial '\"job_id\":\"testmoxin.echo-e2e00001\"'

  run wait_for_record "job start --source moxy --label testmoxin.echo" 10
  assert_success
  run wait_for_record "job done testmoxin.echo-e2e00001 --state succeeded" 10
  assert_success

  run cat "$CLOWN_RECORD"
  assert_output --partial "testmoxin.echo: hello async (madder "
  assert_output --partial "--result-ref moxy async-result testmoxin.echo-e2e00001"

  # The digest in the done line resolves in the user-level store: the
  # stored blob is the full marshaled ToolCallResultV1.
  local digest
  digest=$(grep -oE 'madder [^)]+' "$CLOWN_RECORD" | head -1 | cut -d' ' -f2)
  [ -n "$digest" ]
  run "${MADDER_BIN:-madder}" cat "$digest"
  assert_success
  assert_output --partial "hello async"
}

# Ask-tier tools cannot background: there is no client to prompt once
# detached, so the allow-only preflight rejects synchronously and the clown
# channel is never touched.
function async_rejects_ask_tier_tool { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"async","arguments":{"tool":"testmoxin.asky","args":{}}}'
  assert_success
  assert_output --partial "resolve to allow"
  [ ! -f "$CLOWN_RECORD" ]
}

# permit-async = false forbids backgrounding even at allow tier (#317) —
# distinct rejection text, and the clown channel is never touched.
function async_rejects_permit_async_false_tool { # @test
  run_moxy_mcp "tools/call" \
    '{"name":"async","arguments":{"tool":"testmoxin.noasync","args":{}}}'
  assert_success
  assert_output --partial "permit-async = false"
  [ ! -f "$CLOWN_RECORD" ]
}

# Disabled-channel contract: `clown job start` exiting 0 with EMPTY stdout
# (the CLOWN_DISABLE_JOB_WAKEUP=1 signature) must mint a local id of the
# same shape — async keeps working as a poll surface.
function async_mints_local_id_when_channel_disabled { # @test
  cat > "$HOME/bin/clown" <<'EOF'
#!/bin/sh
exit 0
EOF
  chmod +x "$HOME/bin/clown"

  run_moxy_mcp "tools/call" \
    '{"name":"async","arguments":{"tool":"testmoxin.echo","args":{}}}'
  assert_success
  assert_output --partial '\"status\":\"running\"'
  # Minted shape: <label>-<8hex> with dots preserved.
  echo "$output" | grep -qE '\\\"job_id\\\":\\\"testmoxin\.echo-[0-9a-f]{8}\\\"'
}
