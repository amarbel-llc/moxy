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

# 1700000000 is a well-known epoch: 2023-11-14T22:13:20Z, a Tuesday. Asserting
# the exact conversion is the point — the tool exists because in-head math was
# wrong (#336).
function date_converts_epoch_seconds { # @test
  run "$BIN/date" "1700000000"
  assert_success
  run jq -e 'length == 1 and .[0].epoch == 1700000000 and .[0].utc == "2023-11-14T22:13:20Z"' <<<"$output"
  assert_success
}

function date_weekday_is_correct { # @test
  run "$BIN/date" "1700000000"
  assert_success
  assert_equal "$(jq -r '.[0].weekday' <<<"$output")" "Tuesday"
}

function date_detects_milliseconds { # @test
  # 13+ digits ⇒ milliseconds; scaled back to the same second-epoch.
  run "$BIN/date" "1700000000000"
  assert_success
  run jq -e '.[0].epoch == 1700000000 and .[0].utc == "2023-11-14T22:13:20Z"' <<<"$output"
  assert_success
}

function date_batch_comma_separated { # @test
  run "$BIN/date" "1700000000,1699999999"
  assert_success
  run jq -e 'length == 2 and .[0].epoch == 1700000000 and .[1].epoch == 1699999999' <<<"$output"
  assert_success
}

function date_batch_newline_separated { # @test
  run "$BIN/date" $'1700000000\n1699999999'
  assert_success
  run jq -e 'length == 2' <<<"$output"
  assert_success
}

function date_parses_iso_string { # @test
  run "$BIN/date" "2023-11-14T22:13:20Z"
  assert_success
  run jq -e '.[0].epoch == 1700000000' <<<"$output"
  assert_success
}

function date_explicit_timezone_offset { # @test
  # New York is EST (-0500) or EDT (-0400) depending on DST — exercises the
  # TZDIR-pinned convert path. November is EST.
  run "$BIN/date" "1700000000" "America/New_York"
  assert_success
  off=$(jq -r '.[0].offset' <<<"$output")
  [[ $off == "-0500" || $off == "-0400" ]]
}

function date_invalid_input_errors { # @test
  run "$BIN/date" "not-a-date"
  assert_failure
  assert_output --partial "cannot parse"
}

function date_missing_input_errors { # @test
  run "$BIN/date"
  assert_failure
  assert_output --partial "input required"
}

function date_rejects_path_traversal_tz { # @test
  run "$BIN/date" "1700000000" "../../etc/passwd"
  assert_failure
  assert_output --partial "invalid timezone"
}
