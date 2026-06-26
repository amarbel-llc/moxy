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

# Regression test for #346: tree.toml declared result-type = "text" while the
# script emits a full MCP envelope, so moxy wrapped the envelope JSON as the
# text payload — agents saw `{"content":[...]}` instead of the listing. With
# result-type defaulting to mcp-result (matching ls.toml), the parsed listing
# surfaces directly.
function folio_tree_returns_listing_not_wrapped_envelope { # @test
  mkdir -p "$HOME/project/sub"
  : >"$HOME/project/top.txt"
  : >"$HOME/project/sub/nested.txt"
  cd "$HOME/project"

  local params='{"name":"folio.tree","arguments":{"path":"."}}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  echo "$output" | jq -e '.isError != true' || fail 'tree returned isError: '"$output"

  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  # The listing itself, not a re-serialized envelope.
  case "$text" in
    '{'*) fail "tree double-wrapped its envelope: $text" ;;
  esac
  echo "$text" | grep -q "top.txt" || fail "listing missing top.txt: $text"
  echo "$text" | grep -q "nested.txt" || fail "listing missing nested.txt: $text"
}

# Regression test for #342: a tree listing larger than Linux's per-argument
# exec limit (MAX_ARG_STRLEN, 128 KiB) killed the wrapper because the listing
# was passed to the envelope jq as an argv argument ("Argument list too long").
function folio_tree_large_listing { # @test
  mkdir -p "$HOME/project/big"
  cd "$HOME/project"
  # ~3000 entries x ~58-char lines ≈ 175 KB of tree output.
  local i
  for i in $(seq 3000); do
    : >"big/file-with-a-long-padding-name-to-inflate-listing-$i"
  done

  local params='{"name":"folio.tree","arguments":{"path":"big"}}'
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  echo "$output" | jq -e '.isError != true' || fail 'tree returned isError: '"$output"
  echo "$output" | jq -e '.content | length > 0' || fail '.content | length > 0 check failed: '"$output"
}
