#! /usr/bin/env bats

# bats file_tags=folio

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  # MOXIN_PATH inherited from justfile
}

teardown() {
  teardown_test_home
}

# Regression test for #342: a tree listing larger than Linux's per-argument
# exec limit (MAX_ARG_STRLEN, 128 KiB) killed the wrapper because the listing
# was passed to the envelope jq as an argv argument ("Argument list too long").
#
# Note: the asserted shape is success + non-error content only — folio.tree
# currently double-wraps its envelope (result-type "text" vs envelope-emitting
# script, #346), so this test deliberately does not parse the listing itself.
function folio_tree_large_listing { # @test
  mkdir -p "$HOME/project/big"
  cd "$HOME/project"
  # ~3000 entries x ~58-char lines ≈ 175 KB of tree output.
  local i
  for i in $(seq 3000); do
    : > "big/file-with-a-long-padding-name-to-inflate-listing-$i"
  done

  local params='{"name":"folio.tree","arguments":{"path":"big"}}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.isError != true' || fail 'tree returned isError: '"$output"
  echo "$output" | jq -e '.content | length > 0' || fail '.content | length > 0 check failed: '"$output"
}
