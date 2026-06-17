#! /usr/bin/env bats

# bats file_tags=folio
#
# Regression for #366: folio.write must treat `content` as verbatim bytes.
# moxy rewrites madder://blobs/<digest> argv references to /dev/fd/N pipes so
# tools can stream cached results by URI. That rewrite previously also fired
# on any madder:// URI appearing *inside* the content payload, corrupting
# files whose literal text contains such a URI (test fixtures, docs about the
# URI scheme, JSON records). folio.write opts out of URI substitution
# (substitute-result-uris=false), so content reaches disk unmodified.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

function folio_write_preserves_literal_madder_uri_in_content { # @test
  mkdir -p "$HOME/project"
  cd "$HOME/project"

  # Content with literal madder://blobs/<digest> URIs that must be written
  # verbatim, not rewritten to /dev/fd/N. Two distinct URIs mirror the
  # original report, where they became /dev/fd/3 and /dev/fd/4.
  local content='{"result_ref":"/dev/fd/3","note":"see /dev/fd/4"}'

  local params
  params=$(jq -cn --arg n "folio.write" --arg p "out.json" --arg c "$content" \
    '{name: $n, arguments: {file_path: $p, content: $c}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  # The bytes on disk must be the content verbatim — no /dev/fd/N rewrite.
  local written
  written=$(cat "$HOME/project/out.json")
  assert_equal "$written" "$content"
}
