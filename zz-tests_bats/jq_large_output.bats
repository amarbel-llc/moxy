#! /usr/bin/env bats

# bats file_tags=jq

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  # MOXIN_PATH inherited from justfile
}

teardown() {
  teardown_test_home
}

# Regression test for #342: a filter result larger than Linux's per-argument
# exec limit (MAX_ARG_STRLEN, 128 KiB) killed the wrapper because the result
# was passed to the envelope jq as an argv argument ("Argument list too long").
function jq_large_filter_output { # @test
  # range(40000) with -n -c emits ~230 KB of newline-separated integers.
  local params='{"name":"jq.jq","arguments":{"filter":"range(40000)","flags":["-n","-c"]}}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.isError != true' || fail 'jq returned isError: '"$output"
  echo "$output" | jq -e '.content | length > 0' || fail '.content | length > 0 check failed: '"$output"
}
