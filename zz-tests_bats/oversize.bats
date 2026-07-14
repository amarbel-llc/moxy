#! /usr/bin/env bats

# bats file_tags=oversize

# Regression coverage for #275: a child server that returns a single
# newline-delimited JSON-RPC line larger than go-mcp's old 1 MiB scanner cap
# used to wedge the child permanently ("child process X exited unexpectedly"
# until /mcp reconnect). moxy's own bounded stdio reader raises the ceiling to
# a tunable 64 MiB default and, on a genuine overflow, fails with a clear,
# self-diagnosing error instead of a cryptic wedge.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  FIXTURES_DIR="$(cd "$BATS_TEST_DIRNAME/test-fixtures" && pwd)"
}

teardown() {
  teardown_test_home
}

# A ~2 MiB single-line response (above the old 1 MiB wall, below the 64 MiB
# default ceiling) must be delivered, not dropped. Before the fix this call
# returned an error / empty result because the child was torn down mid-stream.
function oversize_large_response_is_delivered { # @test
  mkdir -p "$HOME/repo"
  cat >"$HOME/repo/moxyfile" <<EOF
[[servers]]
name = "bigline"
command = ["bash", "$FIXTURES_DIR/bigline-server.bash"]
EOF

  cd "$HOME/repo"
  # BIGLINE_BYTES is inherited by moxy and, in turn, by the child it spawns.
  export BIGLINE_BYTES=2097152
  run_moxy_mcp tools/call '{"name":"bigline.big","arguments":{}}'
  assert_success
  # A result came back at all (the helper extracts .result for id==2; a wedged
  # child yields an error response and thus empty output).
  [[ -n $output ]] || fail "no result returned — child likely wedged on the large line"
  echo "$output" | jq -e '.content | length > 0' ||
    fail "result has no content: $output"
  echo "$output" | jq -e '.isError != true' ||
    fail "result reported isError: $output"
}
