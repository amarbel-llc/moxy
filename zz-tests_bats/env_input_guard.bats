#!/usr/bin/env bats

# bats file_tags=env

load 'common'

BIN="${ENV_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/env/bin}"

setup() {
  setup_test_home
}

teardown() {
  teardown_test_home
}

# env_get: allowlist, and the output says when something was redacted (moxy#497)

function get_allowlisted_name_prints_its_value { # @test
  HOME=/home/guard-test run "$BIN/get" HOME
  assert_success
  assert_output "/home/guard-test"
}

function get_allowlisted_but_unset_name_fails_without_redaction_wording { # @test
  run env -u SPINCLASS_SESSION_ID "$BIN/get" SPINCLASS_SESSION_ID
  assert_failure
  refute_output --partial "redacted"
}

function get_non_allowlisted_name_is_redacted_not_unset { # @test
  SECRET_FOR_TEST=hunter2 run "$BIN/get" SECRET_FOR_TEST
  assert_success
  assert_output "redacted: not on the env_get allowlist"
}

function get_non_allowlisted_absent_name_is_also_redacted { # @test
  run env -u NO_SUCH_VARIABLE_FOR_TEST "$BIN/get" NO_SUCH_VARIABLE_FOR_TEST
  assert_success
  assert_output "redacted: not on the env_get allowlist"
}

function get_without_name_lists_only_allowlisted_values_and_counts_the_rest { # @test
  SECRET_FOR_TEST=hunter2 SSH_AUTH_SOCK=/run/agent run "$BIN/get"
  assert_success
  assert_line --regexp '^PATH='
  assert_line --regexp '^HOME='
  refute_output --partial "hunter2"
  refute_output --partial "SECRET_FOR_TEST"
  refute_output --partial "SSH_AUTH_SOCK"
  assert_line --regexp '^[0-9]+ other variables redacted \(not on the env_get allowlist\)$'
}

function get_leading_dash_name_is_refused { # @test
  run "$BIN/get" -0
  assert_failure
  assert_output --partial "must not start with"
}

function get_refuses_more_than_one_name { # @test
  run "$BIN/get" HOME PATH
  assert_failure
}

# env_which: no slash, no leading dash (moxy#497)

function which_plain_name_still_resolves { # @test
  run "$BIN/which" bash
  assert_success
  assert_output --regexp '/bash$'
}

function which_name_with_slash_is_refused { # @test
  run "$BIN/which" /bin/sh
  assert_failure
  assert_output --partial "must not contain"
  run "$BIN/which" ../../../bin/sh
  assert_failure
  assert_output --partial "must not contain"
}

function which_option_style_name_is_refused { # @test
  run "$BIN/which" --all
  assert_failure
  assert_output --partial "must not start with"
}

# env_resolve: /proc, /sys and /dev are refused inside the tool, `--` guards options

function resolve_ordinary_path_still_resolves { # @test
  mkdir -p "$BATS_TEST_TMPDIR/d"
  run "$BIN/resolve" "$BATS_TEST_TMPDIR/d/../d"
  assert_success
  assert_output "$(realpath "$BATS_TEST_TMPDIR/d")"
}

function resolve_refuses_proc_sys_and_dev { # @test
  for p in /proc/self/cwd /proc/1/exe /proc /sys/kernel /dev/null /dev; do
    run "$BIN/resolve" "$p"
    assert_failure
    assert_output --partial "refused"
    run "$BIN/resolve" "$p" target
    assert_failure
    assert_output --partial "refused"
  done
}

function resolve_refuses_traversal_into_proc { # @test
  run "$BIN/resolve" "/tmp/../proc/self/cwd"
  assert_failure
  assert_output --partial "refused"
}

function resolve_refuses_a_symlink_into_proc { # @test
  ln -s /proc/self "$BATS_TEST_TMPDIR/link"
  run "$BIN/resolve" "$BATS_TEST_TMPDIR/link/cwd"
  assert_failure
  assert_output --partial "refused"
  run "$BIN/resolve" "$BATS_TEST_TMPDIR/link" target
  assert_failure
  assert_output --partial "refused"
}

function resolve_option_style_path_is_a_path_not_an_option { # @test
  cd "$BATS_TEST_TMPDIR"
  run "$BIN/resolve" "--version"
  assert_success
  assert_output "$BATS_TEST_TMPDIR/--version"
  refute_output --partial "realpath ("
}
