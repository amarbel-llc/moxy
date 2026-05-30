#! /usr/bin/env bats

# bats file_tags=batch

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

# Helper: create an inline moxin tree with one always-allow tool.
# Sets MOXIN_PATH so the test moxin is discoverable by both the
# spawned moxy and the permcheck resolver inside it.
_setup_batch_fixture() {
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir/echo-srv"
  cat >"$moxin_dir/echo-srv/_moxin.toml" <<'EOF'
schema = 1
name = "echo-srv"
description = "always-allow echo for batch tests"
EOF
  cat >"$moxin_dir/echo-srv/say.toml" <<'EOF'
schema = 1
description = "Echo a message"
command = "echo"
args = ["-n", "hello"]
perms-request = "always-allow"
EOF
  export MOXIN_PATH="$moxin_dir"
}

function batch_tool_listed_with_destructive_hint { # @test
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  # V1 protocol (2025-11-25) preserves tool annotations; V0 strips them.
  run_moxy_mcp_v1 tools/list
  assert_success
  # batch tool listed, gated by destructiveHint annotation so MCP
  # clients prompt the user before each invocation.
  echo "$output" | jq -e '.tools[] | select(.name == "batch")'
  echo "$output" | jq -e '.tools[] | select(.name == "batch") | .annotations.destructiveHint == true'
  echo "$output" | jq -e '.tools[] | select(.name == "batch") | .annotations.readOnlyHint == false'
}

function batch_happy_path_two_moxin_calls { # @test
  _setup_batch_fixture
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  local params
  params=$(jq -cn '{
    name: "batch",
    arguments: {
      calls: [
        {tool: "echo-srv.say", args: {}},
        {tool: "echo-srv.say", args: {}}
      ]
    }
  }')
  run_moxy_mcp tools/call "$params"
  assert_success

  local text
  text=$(echo "$output" | jq -r '.content[0].text')

  # Expect 2 test records and a summary saying both passed.
  local count_tests
  count_tests=$(echo "$text" | jq -c 'select(.type == "test")' | wc -l | tr -d ' ')
  [ "$count_tests" -eq 2 ]

  local passed
  passed=$(echo "$text" | jq -r 'select(.type == "summary") | .passed')
  [ "$passed" = "2" ]
}

function batch_preflight_deny_aborts_on_unknown_tool { # @test
  _setup_batch_fixture
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  # `restart` is a builtin (no perms-request) — resolver returns Unknown
  # → preflight bailout, no sub-calls executed.
  local params
  params=$(jq -cn '{
    name: "batch",
    arguments: {
      calls: [{tool: "restart", args: {}}]
    }
  }')
  run_moxy_mcp tools/call "$params"
  assert_success

  # IsError set on the ToolCallResultV1.
  echo "$output" | jq -e '.isError == true'

  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  # NDJSON stream contains a bailout record.
  echo "$text" | jq -e 'select(.type == "bailout")'
  # Summary marks the batch as bailed and invalid.
  echo "$text" | jq -e 'select(.type == "summary") | .bailed == true'
  echo "$text" | jq -e 'select(.type == "summary") | .valid == false'
}

function batch_rejects_empty_calls_array { # @test
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  run_moxy_mcp tools/call '{"name":"batch","arguments":{"calls":[]}}'
  assert_success
  echo "$output" | jq -e '.isError == true'
  echo "$output" | jq -e '.content[0].text | test("non-empty")'
}

function batch_rejects_malformed_args { # @test
  mkdir -p "$HOME/repo"
  cd "$HOME/repo"
  # Missing required "calls" field. Tier 1 error: parsed but empty.
  run_moxy_mcp tools/call '{"name":"batch","arguments":{}}'
  assert_success
  echo "$output" | jq -e '.isError == true'
}
