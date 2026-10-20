#!/usr/bin/env bats

# bats file_tags=server_perms

# Serve-time permission enforcement (moxy#486, FDR 0015). Under the default
# permission-mode (delegate) moxy's permission tiers are enforced only by the
# Claude Code hooks: a client without them gets a plain tools/call through
# the funnel, which checks --expose, the exclude set and allow-tools and then
# dispatches (moxy#485). With `permission-mode = "enforce"` in the moxyfile
# the funnel resolves each tool's perms-request itself: allow runs; ask,
# deny and a tool with no tier are refused with a "moxy denied" result —
# unless enforce-allow grants the unannotated tool.

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
  cat >"$moxin/open.toml" <<'EOF'
schema = 3
result-type = "text"
content-type = "text/plain"
perms-request = "always-allow"
description = "always-allow tool"
command = "bash"
args = ["-c", "printf 'ran always-allow'"]

[input]
type = "object"
EOF
  cat >"$moxin/plain.toml" <<'EOF'
schema = 3
result-type = "text"
content-type = "text/plain"
description = "tool with no perms-request"
command = "bash"
args = ["-c", "printf 'ran plain'"]

[input]
type = "object"
EOF
  export MOXIN_PATH="$HOME/moxins"
}

teardown() {
  teardown_test_home
}

# The per-test $HOME is also moxy's working directory (run_moxy_mcp), so
# $HOME/moxyfile is the project-local layer of the hierarchy.
enforce_moxyfile() {
  cat >"$HOME/moxyfile" <<'EOF'
builtin-native = false
permission-mode = "enforce"
EOF
}

function delegate_is_the_default_and_runs_an_each_use_tool { # @test
  run_moxy_mcp "tools/call" '{"name":"permfix.asky","arguments":{}}'
  assert_success
  assert_output --partial "ran each-use"
}

function enforce_does_not_run_an_each_use_tool { # @test
  enforce_moxyfile
  run_moxy_mcp "tools/call" '{"name":"permfix.asky","arguments":{}}'
  assert_success
  refute_output --partial "ran each-use"
  assert_output --partial '"isError":true'
  assert_output --partial "moxy denied permfix.asky"
  assert_output --partial "nobody can answer"
}

function enforce_does_not_run_a_dynamic_deny_tool { # @test
  enforce_moxyfile
  run_moxy_mcp "tools/call" '{"name":"permfix.dyndeny","arguments":{}}'
  assert_success
  refute_output --partial "ran dynamic-deny"
  assert_output --partial "moxy denied permfix.dyndeny"
}

function enforce_runs_an_always_allow_tool { # @test
  enforce_moxyfile
  run_moxy_mcp "tools/call" '{"name":"permfix.open","arguments":{}}'
  assert_success
  assert_output --partial "ran always-allow"
}

function enforce_refuses_a_tool_with_no_tier_unless_granted { # @test
  enforce_moxyfile
  run_moxy_mcp "tools/call" '{"name":"permfix.plain","arguments":{}}'
  assert_success
  refute_output --partial "ran plain"
  assert_output --partial "declares no permission tier"

  cat >>"$HOME/moxyfile" <<'EOF'
enforce-allow = ["permfix.plain"]
EOF
  run_moxy_mcp "tools/call" '{"name":"permfix.plain","arguments":{}}'
  assert_success
  assert_output --partial "ran plain"
}

function enforce_is_locked_under_narrowing { # @test
  mkdir -p "$HOME/parent" "$HOME/child"
  cat >"$HOME/parent/moxyfile" <<'EOF'
builtin-native = false
permission-mode = "enforce"
enforce-allow = ["permfix.plain"]
EOF
  cat >"$HOME/child/moxyfile" <<'EOF'
builtin-native = false
enforce-allow = ["permfix"]
EOF
  run_moxy narrows --parent "$HOME/parent/moxyfile" --child "$HOME/child/moxyfile"
  assert_failure 1
  assert_output "enforce-allow: child grants permfix under enforce, parent does not
permission-mode: parent enforces, child does not"
}

function an_unknown_permission_mode_is_refused_at_load { # @test
  cat >"$HOME/moxyfile" <<'EOF'
permission-mode = "ask"
EOF
  run_moxy status
  assert_failure
  assert_output --partial "permission-mode"
}
