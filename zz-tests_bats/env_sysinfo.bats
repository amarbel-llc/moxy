#!/usr/bin/env bats

# bats file_tags=env

load 'common'

BIN="${ENV_BIN:-$BATS_TEST_DIRNAME/../result/share/moxy/moxins/env/bin}"

setup() {
  setup_test_home
  [[ -r /proc/loadavg ]] || skip "no /proc (Linux-only tools)"
}

teardown() {
  teardown_test_home
}

function loadavg_emits_required_fields { # @test
  run "$BIN/loadavg"
  assert_success
  run jq -e 'has("load1") and has("load5") and has("load15") and has("running") and has("total") and (.cpus >= 1) and (.per_cpu | has("load1"))' <<<"$output"
  assert_success
}

function loadavg_per_cpu_is_load_over_cpus { # @test
  run "$BIN/loadavg"
  assert_success
  run jq -e '(.per_cpu.load1 * .cpus - .load1) | fabs < 0.0001' <<<"$output"
  assert_success
}

function uptime_emits_required_fields { # @test
  run "$BIN/uptime"
  assert_success
  run jq -e '(.uptime_sec > 0) and (.idle_sec >= 0) and (.boot_time_utc | endswith("Z"))' <<<"$output"
  assert_success
}

function meminfo_emits_byte_counts { # @test
  run "$BIN/meminfo"
  assert_success
  run jq -e '(.total > 0) and (.available <= .total) and (.used == (.total - .available)) and (.swap_used == (.swap_total - .swap_free))' <<<"$output"
  assert_success
}

function cpu_emits_percentages { # @test
  run "$BIN/cpu" 50
  assert_success
  run jq -e '(.interval_ms == 50) and (.total_busy_pct >= 0 and .total_busy_pct <= 100) and (.per_cpu | length >= 1) and ([.per_cpu[]] | all(. >= 0 and . <= 100))' <<<"$output"
  assert_success
}

function cpu_rejects_non_numeric_interval { # @test
  run "$BIN/cpu" abc
  assert_failure
  assert_output --partial "must be a non-negative integer"
}

function cpu_rejects_out_of_range_interval { # @test
  run "$BIN/cpu" 5
  assert_failure
  assert_output --partial "must be between 10 and 5000"
  run "$BIN/cpu" 6000
  assert_failure
  assert_output --partial "must be between 10 and 5000"
}
