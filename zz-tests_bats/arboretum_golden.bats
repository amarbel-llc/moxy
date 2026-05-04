#! /usr/bin/env bats

# Diff each fixture's outline against its captured golden output. Goldens are
# committed under zz-tests_bats/test-fixtures/arboretum/ and re-captured via
# `just debug-arboretum-regen-goldens` when grammar drift is intentional.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

# Diff helper used by every per-language @test below.
golden_test() {
  local ext="$1"
  local fixture_dir="$BATS_TEST_DIRNAME/test-fixtures/arboretum"
  local fixture="$fixture_dir/sample.$ext"
  local golden="$fixture_dir/sample.$ext.golden"

  cp "$fixture" "$HOME/sample.$ext"

  local params
  params=$(jq -cn --arg n "arboretum.outline" --arg p "$HOME/sample.$ext" \
    '{name: $n, arguments: {path: $p}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local got
  got=$(echo "$output" | jq -r '.content[0].resource.text // .content[0].text' \
    | sed "s|$HOME/sample.$ext|samples/sample.$ext|g")

  diff <(echo "$got") "$golden"
}

function arboretum_golden_go { # @test
  golden_test go
}

function arboretum_golden_ts { # @test
  golden_test ts
}

function arboretum_golden_js { # @test
  golden_test js
}

function arboretum_golden_rs { # @test
  golden_test rs
}

function arboretum_golden_py { # @test
  golden_test py
}

function arboretum_golden_php { # @test
  golden_test php
}

function arboretum_golden_sh { # @test
  golden_test sh
}
