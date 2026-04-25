#! /usr/bin/env bats

# Added for moxy POC dynamic-perms
#
# Wrapper around the self-asserting driver. The driver hardcodes its own
# pass/fail and exits 0/1 — bats just times it out and checks the exit code.

function dynamic_perms_poc_driver_passes { # @test
  local bin="${BATS_TEST_DIRNAME}/../build/moxy-exporel-dynamic-perms"
  run timeout 30 "$bin"
  [ "$status" -eq 0 ]
}
