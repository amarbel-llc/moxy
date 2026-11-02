#! /usr/bin/env bash
# Shared helpers for the policy-broker lanes (moxy#492): a real stand-in
# broker process (test-fixtures/policy-broker/stand_in_broker.py) and the
# PreToolUse hook run as a real subprocess against it.

BROKER_FIXTURES="$BATS_TEST_DIRNAME/test-fixtures/policy-broker"

# start_broker MODE [KEY] [LISTEN] [ALLOWED]: LISTEN defaults to a socket under
# the test's tmp dir; "tcp" binds loopback. ALLOWED is the policy mode's
# comma-separated allow list. Sets BROKER_ADDR and BROKER_LOG.
start_broker() {
  local mode="$1" key="${2:-sess}" listen="${3:-$BATS_TEST_TMPDIR/broker.sock}" allowed="${4:-}"
  BROKER_LOG="$BATS_TEST_TMPDIR/broker.log"
  local ready="$BATS_TEST_TMPDIR/broker.ready"
  : >"$BROKER_LOG"
  python3 "$BROKER_FIXTURES/stand_in_broker.py" "$listen" "$mode" "$key" \
    "$BROKER_LOG" "$ready" "$allowed" &
  BROKER_PID=$!
  local i
  for i in $(seq 100); do
    [[ -f $ready ]] && break
    sleep 0.05
  done
  [[ -f $ready ]] || fail "stand-in broker ($mode) did not come up"
  BROKER_ADDR=$(<"$ready")
}

stop_broker() {
  [[ -n ${BROKER_PID:-} ]] || return 0
  kill "$BROKER_PID" 2>/dev/null || true
  wait "$BROKER_PID" 2>/dev/null || true
}

# hook_decision FLOOR ADDR [SESSION_KEY]: run `moxy hook` on a dontAsk
# PermissionRequest for the resource-bearing fixture tool and print the
# behavior it chose. FLOOR is "allow" or "ask" (what the static floor says);
# under dontAsk an ask floor comes out as deny. ADDR "" leaves
# CLOWN_POLICY_BROKER unset.
hook_decision() {
  local floor="$1" addr="$2" key="${3:-}"
  local input
  input=$(jq -cn '{
    hook_event_name: "PermissionRequest",
    session_id: "broker-lane",
    tool_name: "mcp__plugin_moxy_moxy__dynres-srv_echo",
    tool_input: {path: "/outside/tree"},
    cwd: ".",
    permission_mode: "dontAsk"
  }')
  mkdir -p "$HOME/project"
  run env -C "$HOME/project" \
    MOXIN_PATH="$BROKER_FIXTURES/moxins-floor-$floor" \
    CLOWN_POLICY_BROKER="$addr" SPINCLASS_SESSION_ID="$key" \
    timeout --preserve-status 5s bash -c \
    'echo "$1" | "${MOXY_BIN:-moxy}" hook' -- "$input"
  assert_success
  HOOK_BEHAVIOR=$(jq -r '.hookSpecificOutput.decision.behavior // "none"' <<<"$output")
}

# hook_behavior TOOL INPUT_JSON [ADDR]: run `moxy hook` on a dontAsk
# PermissionRequest for a real moxin (MOXIN_PATH as the lane provides it) from
# $HOME/project, and set HOOK_BEHAVIOR to allow, deny or none.
hook_behavior() {
  local tool="$1" tool_input="$2" addr="${3:-}"
  local input
  input=$(jq -cn --arg tn "$tool" --argjson ti "$tool_input" '{
    hook_event_name: "PermissionRequest",
    session_id: "broker-lane",
    tool_name: $tn,
    tool_input: $ti,
    cwd: ".",
    permission_mode: "dontAsk"
  }')
  mkdir -p "$HOME/project"
  run env -C "$HOME/project" CLOWN_POLICY_BROKER="$addr" SPINCLASS_SESSION_ID="" \
    timeout --preserve-status 5s bash -c \
    'echo "$1" | "${MOXY_BIN:-moxy}" hook' -- "$input"
  assert_success
  HOOK_BEHAVIOR=$(jq -r '.hookSpecificOutput.decision.behavior // "none"' <<<"$output")
}

broker_checks() { jq -c 'select(.type == "check")' "$BROKER_LOG"; }
