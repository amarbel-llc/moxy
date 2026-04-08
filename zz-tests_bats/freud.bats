#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function resource_templates_listed { # @test
  run_freud_mcp resources/templates/list
  assert_success
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "freud://sessions")'
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate == "freud://sessions/{project}")'
}
