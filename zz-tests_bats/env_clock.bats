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

function clock_emits_all_required_fields { # @test
  run "$BIN/clock"
  assert_success
  run jq -e 'has("local") and has("utc") and has("timezone") and has("offset") and has("unix")' <<<"$output"
  assert_success
}

function clock_unix_is_a_number { # @test
  run "$BIN/clock"
  assert_success
  run jq -e '.unix | type == "number"' <<<"$output"
  assert_success
}

function clock_utc_field_is_zulu { # @test
  run "$BIN/clock"
  assert_success
  utc=$(jq -r '.utc' <<<"$output")
  [[ "$utc" == *Z ]]
}

function clock_explicit_utc_timezone { # @test
  run "$BIN/clock" "UTC"
  assert_success
  assert_equal "$(jq -r '.timezone' <<<"$output")" "UTC"
  assert_equal "$(jq -r '.offset' <<<"$output")" "+0000"
}

function clock_explicit_iana_timezone_offset { # @test
  # New York is either EST (-0500) or EDT (-0400) depending on DST. This
  # exercises the convert path end-to-end through the TZDIR-pinned wrapper.
  run "$BIN/clock" "America/New_York"
  assert_success
  assert_equal "$(jq -r '.timezone' <<<"$output")" "America/New_York"
  off=$(jq -r '.offset' <<<"$output")
  [[ "$off" == "-0500" || "$off" == "-0400" ]]
}

function clock_invalid_timezone_errors { # @test
  run "$BIN/clock" "Mars/Phobos"
  assert_failure
  assert_output --partial "unknown timezone"
}

function clock_rejects_path_traversal { # @test
  run "$BIN/clock" "../../etc/passwd"
  assert_failure
  assert_output --partial "invalid timezone"
}
