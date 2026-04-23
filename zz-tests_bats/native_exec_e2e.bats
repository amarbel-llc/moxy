#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output

  # Isolate cache inside the test home so sessions share the same disk cache.
  export XDG_CACHE_HOME="$HOME/.cache"

  # Set up a native shell exec tool via MOXIN_PATH.
  local moxin_dir="$HOME/project/.moxy/moxins"
  mkdir -p "$moxin_dir/shell"
  cat >"$moxin_dir/shell/_moxin.toml" <<'EOF'
schema = 1
name = "shell"
EOF
  cat >"$moxin_dir/shell/exec.toml" <<'EOF'
schema = 1
description = "Execute a shell command"
command = "sh"
args = ["-c"]

[input.properties.command]
type = "string"
description = "Shell command to execute"

[input]
required = ["command"]
EOF

  export MOXIN_PATH="$moxin_dir"
  cd "$HOME/project"
}

teardown() {
  teardown_test_home
}

function native_exec_runs_command_from_arguments { # @test
  local params='{"name":"shell.exec","arguments":{"command":"echo -n hello"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "hello"
}

function native_exec_passes_arguments_to_sh_c { # @test
  # Verify that a command with pipes and shell features works correctly
  # through the sh -c invocation path.
  local params='{"name":"shell.exec","arguments":{"command":"echo one two three | wc -w | tr -d \" \""}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "3"
}

function native_exec_caches_large_output { # @test
  # Generate output exceeding the 200-token threshold (~800 chars).
  local params='{"name":"shell.exec","arguments":{"command":"seq 1 1000"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  # Summary should contain the resource URI.
  echo "$output" | jq -er '.content[0].text' | grep -q 'moxy\.native://results/'

  # Summary should contain head/tail markers.
  echo "$output" | jq -er '.content[0].text' | grep -q 'First 10 lines'
  echo "$output" | jq -er '.content[0].text' | grep -q 'Last 10 lines'
}

function native_exec_cache_layout_includes_session_directory { # @test
  local params='{"name":"shell.exec","arguments":{"command":"seq 1 1000"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  # Session ID is resolved at startup (UUID fallback when no env var is set).
  # Verify that a session subdirectory was created (not "no-session").
  local session_dir
  session_dir=$(find "$XDG_CACHE_HOME/moxy/native-results" -mindepth 1 -maxdepth 1 -type d | head -1)
  [[ -n $session_dir ]]
  [[ $(basename "$session_dir") != "no-session" ]]
  ls "$session_dir"/*.json >/dev/null
  ls "$session_dir"/*.txt >/dev/null
}

function native_exec_resource_as_fd_substitution { # @test
  # Step 1: Generate large output to get a cached URI.
  local params='{"name":"shell.exec","arguments":{"command":"seq 1 1000"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  local uri
  uri=$(echo "$output" | jq -er '.content[0].text' | grep -oP 'moxy\.native://results/[A-Za-z0-9._-]+/[a-f0-9-]+')
  [[ -n $uri ]]

  # Step 2: Use the cached URI in a grep command via a second moxy session.
  # Both sessions share XDG_CACHE_HOME so the disk cache is visible.
  local cmd="grep -x 42 $uri"
  local call_params
  call_params=$(jq -cn --arg c "$cmd" '{"name":"shell.exec","arguments":{"command":$c}}')
  run_moxy_mcp "tools/call" "$call_params"
  assert_success
  echo "$output" | jq -e '.content[0].text == "42\n"'
}

function native_exec_resource_as_fd_repeated_uri_shares_fd { # @test
  # Step 1: Generate large output.
  local params='{"name":"shell.exec","arguments":{"command":"seq 1 1000"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  local uri
  uri=$(echo "$output" | jq -er '.content[0].text' | grep -oP 'moxy\.native://results/[A-Za-z0-9._-]+/[a-f0-9-]+')
  [[ -n $uri ]]

  # Step 2: Diff the same cached result against itself -- must produce no
  # output and must not deadlock (both references share the same fd).
  local cmd="diff $uri $uri"
  local call_params
  call_params=$(jq -cn --arg c "$cmd" '{"name":"shell.exec","arguments":{"command":$c}}')
  run_moxy_mcp "tools/call" "$call_params"
  assert_success

  # An empty diff means either no content blocks or an empty text block.
  local txt
  txt=$(echo "$output" | jq -r '.content[0].text // ""')
  [[ -z $txt ]]
}

function native_exec_resource_missing_cached_id_errors { # @test
  local params='{"name":"shell.exec","arguments":{"command":"cat moxy.native://results/nonexistent-session/does-not-exist"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  echo "$output" | jq -e '.isError == true'
}

function native_exec_no_arguments_uses_spec_args_only { # @test
  # A tool with no input schema and no arguments should still work
  # (backwards compatible with existing native_server.bats tests).
  mkdir -p "$HOME/project/.moxy/moxins/greeter"
  cat >"$HOME/project/.moxy/moxins/greeter/_moxin.toml" <<'EOF'
schema = 1
name = "greeter"
EOF
  cat >"$HOME/project/.moxy/moxins/greeter/hello.toml" <<'EOF'
schema = 1
description = "Say hello"
command = "echo"
args = ["-n", "hello world"]
EOF

  local params='{"name":"greeter.hello"}'
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "hello world"
}
