#! /usr/bin/env bats

# bats file_tags=name_template

# Unit-level coverage for `moxy render` — the renderer's out-of-process surface
# (forward + structural reverse). Pure CLI, no server. See
# docs/features/0007-name-template.md.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

function render_forward_default_is_dotted { # @test
  run_moxy render --server grit --tool commit
  assert_success
  assert_output --partial "grit.commit"
}

function render_forward_underscore_template { # @test
  run_moxy render --name-template '{server}_{tool}' --server grit --tool commit
  assert_success
  assert_output --partial "grit_commit"
}

function render_reverse_resolves_invertible { # @test
  run_moxy render --name-template '{server}_{tool}' --resolve grit_commit
  assert_success
  # Forward and reverse round-trip: "server<TAB>tool".
  assert_output --partial "grit"
  assert_output --partial "commit"
}

function render_reverse_not_invertible_fails { # @test
  # '{tool}' drops the server, so a rendered name cannot recover it.
  run_moxy render --name-template '{tool}' --resolve commit
  assert_failure
  assert_output --partial "not invertible"
}

function render_missing_tool_template_fails { # @test
  run_moxy render --name-template '{server}' --server grit --tool commit
  assert_failure
}

function render_forward_requires_server_and_tool { # @test
  run_moxy render --name-template '{server}_{tool}'
  assert_failure
}
