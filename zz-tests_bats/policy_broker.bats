#! /usr/bin/env bats

# bats file_tags=policy_broker

# moxy#492: the PreToolUse hook's broker dial against a real unix socket.
# Every no-answer case must land on moxy's static floor (today's behaviour),
# never on a new allow or deny: each runs once with a floor that allows and
# once with a floor that asks (deny under dontAsk), and must reproduce the
# floor both times.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  load "$BATS_TEST_DIRNAME/policy_broker.bash"
  setup_test_home
}

teardown() {
  stop_broker
  teardown_test_home
}

assert_floor() { # ADDR [KEY]
  hook_decision allow "$@"
  [[ $HOOK_BEHAVIOR == allow ]] || fail "floor-allow: got $HOOK_BEHAVIOR"
  hook_decision ask "$@"
  [[ $HOOK_BEHAVIOR == deny ]] || fail "floor-ask: got $HOOK_BEHAVIOR"
}

function no_broker_variable_is_the_floor { # @test
  assert_floor ""
}

function broker_allow_replaces_an_asking_floor { # @test
  start_broker allow
  hook_decision ask "$BROKER_ADDR"
  [[ $HOOK_BEHAVIOR == allow ]] || fail "got $HOOK_BEHAVIOR"
  run broker_checks
  [[ ${#lines[@]} -eq 1 ]]
  jq -e '.op == "read" and .resource.uri != "" and .subject == "sess"' <<<"${lines[0]}"
}

function broker_deny_replaces_an_allowing_floor { # @test
  start_broker deny
  hook_decision allow "$BROKER_ADDR"
  [[ $HOOK_BEHAVIOR == deny ]] || fail "got $HOOK_BEHAVIOR"
}

function broker_ask_replaces_an_allowing_floor { # @test
  start_broker ask
  hook_decision allow "$BROKER_ADDR"
  # ask is final and the floor's allow never ran; dontAsk turns ask into deny
  [[ $HOOK_BEHAVIOR == deny ]] || fail "got $HOOK_BEHAVIOR"
}

function no_socket_file_is_the_floor { # @test
  assert_floor "$BATS_TEST_TMPDIR/absent.sock"
}

function connection_refused_is_the_floor { # @test
  start_broker unlistening sess "$BATS_TEST_TMPDIR/refused.sock"
  assert_floor "$BROKER_ADDR"
}

function broker_that_never_says_hello_is_the_floor { # @test
  start_broker nohello
  assert_floor "$BROKER_ADDR"
}

function broker_that_never_answers_is_the_floor { # @test
  start_broker hang
  assert_floor "$BROKER_ADDR"
  run broker_checks
  [[ ${#lines[@]} -ge 1 ]] # it was asked; the deadline made it no answer
}

function dropped_connection_is_the_floor { # @test
  start_broker drop
  assert_floor "$BROKER_ADDR"
}

function wrong_broker_is_refused_and_the_floor_decides { # @test
  start_broker allow other-session
  assert_floor "$BROKER_ADDR" mine
  run broker_checks
  [[ ${#lines[@]} -eq 0 ]] || fail "a refused broker must never be asked"
}
