#! /usr/bin/env bats

# bats file_tags=net_cap

# moxy#492: CLOWN_POLICY_BROKER as host:port (the confined relay form). Binds
# loopback, hence net_cap; the unix-socket cases are in policy_broker.bats.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  load "$BATS_TEST_DIRNAME/policy_broker.bash"
  setup_test_home
}

teardown() {
  stop_broker
  teardown_test_home
}

function tcp_broker_verdict_replaces_the_floor { # @test
  start_broker deny sess tcp
  [[ $BROKER_ADDR == 127.0.0.1:* ]]
  hook_decision allow "$BROKER_ADDR"
  [[ $HOOK_BEHAVIOR == deny ]] || fail "got $HOOK_BEHAVIOR"
}

function tcp_broker_that_never_answers_is_the_floor { # @test
  start_broker hang sess tcp
  hook_decision allow "$BROKER_ADDR"
  [[ $HOOK_BEHAVIOR == allow ]] || fail "got $HOOK_BEHAVIOR"
}
