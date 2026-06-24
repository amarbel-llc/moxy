#! /usr/bin/env bats

# bats file_tags=arboretum

# Drift gate (moxy#379): the vendored tree-sitter grammars and the
# web-tree-sitter runtime are pinned independently. arboretum.abi-check loads
# every grammar wasm and asserts its ABI version is within the runtime's
# supported range, and that the runtime wasm the bundle loads exists. This test
# fails the build if that alignment drifts.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

function arboretum_abi_check_grammars_in_runtime_range { # @test
  local params
  params=$(jq -cn --arg n "arboretum.abi-check" '{name: $n, arguments: {}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local text
  text=$(echo "$output" | jq -r '.content[0].resource.text // .content[0].text')

  # The report lists the runtime range and every grammar; no grammar may be
  # flagged OUT OF RANGE or LOAD FAILED.
  echo "$text" | grep -q "runtime web-tree-sitter ABI range:"
  echo "$text" | grep -q "bash: abiVersion="
  if echo "$text" | grep -qE "OUT OF RANGE|LOAD FAILED"; then
    echo "abi-check reported grammar drift:" >&2
    echo "$text" >&2
    return 1
  fi
}
