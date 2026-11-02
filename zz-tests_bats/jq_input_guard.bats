#! /usr/bin/env bats

# bats file_tags=jq

# moxy#495: jq's flags cannot name a file (the files policy gates `files`, so a
# --rawfile would be a way around it), and a filter cannot read the process
# environment.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  mkdir -p "$HOME/outside"
  echo '"flag-secret"' >"$HOME/outside/secret.json"
  echo '{"a":1}' >"$HOME/outside/data.json"
}

teardown() {
  teardown_test_home
}

# call_jq FILTER FLAGS_JSON [FILES_JSON]: sets $output to the MCP tool result.
call_jq() {
  local params
  params=$(jq -cn --arg f "$1" --argjson flags "$2" --argjson files "${3:-null}" \
    '{name: "jq.jq", arguments: ({filter: $f, flags: $flags} + (if $files == null then {} else {files: $files} end))}')
  run_moxy_mcp_v1 "tools/call" "$params"
}

# result_text: the text of the result, whether inline or a cached resource.
result_text() { jq -r '.content[0] | (.text // .resource.text // "")' <<<"$output"; }

assert_ok() { jq -e '.isError != true' <<<"$output" >/dev/null || fail "tool failed: $output"; }

function flags_that_read_a_file_are_refused { # @test
  local secret="$HOME/outside/secret.json" flags
  for flags in \
    "[\"-n\",\"--rawfile\",\"x\",\"$secret\"]" \
    "[\"-n\",\"--slurpfile\",\"x\",\"$secret\"]" \
    "[\"-n\",\"--argfile\",\"x\",\"$secret\"]" \
    "[\"-f\",\"$secret\"]" \
    "[\"--from-file\",\"$secret\"]" \
    "[\"-n\",\"-L\",\"$HOME/outside\"]" \
    "[\"-nf\",\"$secret\"]" \
    '["--args"]' \
    '["--jsonargs"]'; do
    call_jq '$x' "$flags"
    assert_success
    jq -e '.isError == true' <<<"$output" >/dev/null || fail "flags $flags were not refused: $output"
    [[ $output != *flag-secret* ]] || fail "flags $flags leaked the file: $output"
  done
}

function a_filter_cannot_read_the_environment { # @test
  export SECRET_FOR_TEST=hunter2
  call_jq '$ENV | keys | length' '["-n"]'
  assert_ok
  [[ $(result_text) == 0 ]] || fail "ENV not empty: $output"
  call_jq 'env.SECRET_FOR_TEST' '["-n"]'
  assert_ok
  [[ $(result_text) == null ]] || fail "env leaked: $output"
}

function a_filter_that_looks_like_an_option_is_still_a_filter { # @test
  call_jq '-1' '["-n"]'
  assert_ok
  [[ $(result_text) == -1 ]] || fail "$output"
}

function a_file_that_looks_like_an_option_is_refused { # @test
  call_jq '.' '[]' '["--rawfile"]'
  assert_success
  jq -e '.isError == true' <<<"$output" >/dev/null || fail "$output"
}

function ordinary_flags_still_work { # @test
  local data="$HOME/outside/data.json"
  call_jq '.a' '["-r"]' "[\"$data\"]"
  assert_ok
  [[ $(result_text) == 1 ]] || fail "$output"
  call_jq '$x + 1' '["-n","-c","--argjson","x","41"]'
  assert_ok
  [[ $(result_text) == 42 ]] || fail "$output"
  call_jq '$s' '["-n","-r","--arg","s","hello"]'
  assert_ok
  [[ $(result_text) == hello ]] || fail "$output"
  call_jq '{b:1,a:2}' '["-n","--indent","1","-S"]'
  assert_ok
  call_jq 'range(3)' '["-nc"]'
  assert_ok
}
