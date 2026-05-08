#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output

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

  # Summary should contain the madder blob URI.
  echo "$output" | jq -er '.content[0].text' | grep -q 'madder://blobs/'

  # Summary should contain head/tail markers.
  echo "$output" | jq -er '.content[0].text' | grep -q 'First 10 lines'
  echo "$output" | jq -er '.content[0].text' | grep -q 'Last 10 lines'
}

function native_exec_blob_lands_in_madder_store { # @test
  # The .default store is at ./.madder/local/share/blob_stores/default/.
  # After a large-output call, at least one blob file must exist there.
  local params='{"name":"shell.exec","arguments":{"command":"seq 1 1000"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  local store_dir="$HOME/.madder/local/share/blob_stores/default"
  [[ -d $store_dir ]]
  # madder lays out blobs in hash-bucketed subdirectories — count any
  # regular file under the store root.
  local count
  count=$(find "$store_dir" -type f -not -name 'blob_store-config' | wc -l)
  [[ $count -gt 0 ]]
}

function native_exec_resource_as_fd_substitution { # @test
  # Step 1: Generate large output to get a cached URI.
  local params='{"name":"shell.exec","arguments":{"command":"seq 1 1000"}}'
  run_moxy_mcp "tools/call" "$params"
  assert_success

  local uri
  uri=$(echo "$output" | jq -er '.content[0].text' | grep -oP 'madder://blobs/[A-Za-z0-9._-]+')
  [[ -n $uri ]]

  # Step 2: Use the cached URI in a grep command via a second moxy
  # session. The second session walks up to the same `.default` store
  # so the digest resolves.
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
  uri=$(echo "$output" | jq -er '.content[0].text' | grep -oP 'madder://blobs/[A-Za-z0-9._-]+')
  [[ -n $uri ]]

  # Step 2: Diff the same cached blob against itself — must produce no
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
  local params='{"name":"shell.exec","arguments":{"command":"cat madder://blobs/blake2b256-deadbeefdeadbeefdeadbeefdeadbeef"}}'
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
