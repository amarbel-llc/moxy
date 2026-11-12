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

function host_emits_virtualization_and_kvm_facts { # @test
  run "$BIN/host"
  assert_success
  run jq -e '
    (.hostname | type == "string" and length > 0)
    and (.kernel | type == "string" and length > 0)
    and (.cpu.count >= 1)
    and (.cpu.flags | keys == ["hypervisor", "svm", "vmx"])
    and ([.cpu.flags[]] | all(type == "boolean"))
    and (.kvm.device_exists | type == "boolean")
    and (.kvm.device_accessible | type == "boolean")
    and (.kvm.module_loaded | type == "boolean")
    and has("dmi") and has("nix_system_features")' <<<"$output"
  assert_success
}

function host_kvm_accessible_implies_exists { # @test
  run "$BIN/host"
  assert_success
  run jq -e '(.kvm.device_accessible | not) or .kvm.device_exists' <<<"$output"
  assert_success
}

function host_says_null_not_error_when_dmi_is_missing { # @test
  # The DMI directory is absent in many containers and VMs; that is a null,
  # not a failure.
  HOST_DMI_DIR="$BATS_TEST_TMPDIR/no-dmi" run "$BIN/host"
  assert_success
  run jq -e '.dmi == null' <<<"$output"
  assert_success
}

function host_reads_dmi_and_nix_features_from_the_given_files { # @test
  mkdir -p "$BATS_TEST_TMPDIR/dmi"
  echo "Acme" >"$BATS_TEST_TMPDIR/dmi/sys_vendor"
  echo "Widget 9" >"$BATS_TEST_TMPDIR/dmi/product_name"
  printf 'build-users-group = nixbld\nsystem-features = nixos-test benchmark big-parallel kvm\n' \
    >"$BATS_TEST_TMPDIR/nix.conf"
  HOST_DMI_DIR="$BATS_TEST_TMPDIR/dmi" HOST_NIX_CONF="$BATS_TEST_TMPDIR/nix.conf" run "$BIN/host"
  assert_success
  run jq -e '.dmi == {vendor: "Acme", product: "Widget 9"}
    and .nix_system_features == ["nixos-test", "benchmark", "big-parallel", "kvm"]' <<<"$output"
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
