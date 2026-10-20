#!/usr/bin/env bats

# bats file_tags=server_perms

# moxy's permission tiers (perms-request) are enforced by the Claude Code
# PreToolUse / PermissionRequest hooks, not by the server. A client that
# reaches moxy without those hooks in front (a juggler agent, any non-Claude
# MCP client) gets a plain tools/call through Proxy.callToolV1, which checks
# --expose, the exclude set and allow-tools, but never the permcheck resolver.
#
# These tests describe the WANTED behaviour: a tool whose tier is not
# always-allow must not run on a plain tools/call with no hook in front.
# They are skipped until the operator chooses a server-side design.

load 'common'

setup() {
  setup_test_home

  local moxin="$HOME/moxins/permfix"
  mkdir -p "$moxin"
  cat >"$moxin/_moxin.toml" <<'EOF'
schema = 1
name = "permfix"
description = "server-side permission fixture"
EOF
  cat >"$moxin/asky.toml" <<'EOF'
schema = 3
result-type = "text"
content-type = "text/plain"
perms-request = "each-use"
description = "each-use tool"
command = "bash"
args = ["-c", "printf 'ran each-use'"]

[input]
type = "object"
EOF
  # The dynamic predicate always answers deny (exit 2).
  cat >"$moxin/dyndeny.toml" <<'EOF'
schema = 3
result-type = "text"
content-type = "text/plain"
perms-request = "dynamic"
description = "dynamic tool whose predicate always denies"
command = "bash"
args = ["-c", "printf 'ran dynamic-deny'"]

[input]
type = "object"

[dynamic-perms]
command = "bash"
args = ["-c", "exit 2"]
EOF
  export MOXIN_PATH="$HOME/moxins"
}

teardown() {
  teardown_test_home
}

function plain_tools_call_does_not_run_each_use_tool { # @test
  skip "moxy does not enforce perms-request server-side yet (moxy#485)"
  run_moxy_mcp "tools/call" '{"name":"permfix.asky","arguments":{}}'
  assert_success
  refute_output --partial "ran each-use"
}

function plain_tools_call_does_not_run_dynamic_deny_tool { # @test
  skip "moxy does not enforce perms-request server-side yet (moxy#485)"
  run_moxy_mcp "tools/call" '{"name":"permfix.dyndeny","arguments":{}}'
  assert_success
  refute_output --partial "ran dynamic-deny"
}
