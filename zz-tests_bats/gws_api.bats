#! /usr/bin/env bats

# bats file_tags=gws

# moxy#515: gws.api carries a request body as one --json argument, so a value
# with spaces survives (the command string is split on whitespace).

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  load 'piers_stub'
  setup_test_home
  setup_gws_stub
}

teardown() {
  teardown_test_home
}

call_api() { # ARGUMENTS_JSON
  mkdir -p "$HOME/project"
  cd "$HOME/project"
  run_moxy_mcp_v1 "tools/call" "$(jq -cn --argjson a "$1" '{name: "gws.api", arguments: $a}')"
  assert_success
}

function json_body_with_spaces_reaches_gws_as_one_argument { # @test
  call_api '{"command": "drive files update", "params": "{\"fileId\": \"doc-1\"}", "json": "{\"name\": \"One isomorphism: a new title\"}"}'
  jq -e '.isError != true' <<<"$output" >/dev/null || fail "tool failed: $output"
  # argv is logged tab-separated, one entry per field
  local expected
  expected=$(printf 'drive\tfiles\tupdate\t--params\t%s\t--json\t%s\t' \
    '{"fileId": "doc-1"}' '{"name": "One isomorphism: a new title"}')
  [[ $(cat "$GWS_STUB_LOG") == "$expected" ]] || fail "argv: $(cat "$GWS_STUB_LOG")"
}

# moxy#458: gws saves a non-JSON reply as download.html in its cwd; that cwd is
# scratch, not the caller's tree.
function gws_runs_from_a_scratch_directory_not_the_callers_tree { # @test
  export GWS_STUB_WRITE_DOWNLOAD=1
  call_api '{"command": "drive files delete", "params": "{\"fileId\": \"doc-1\"}"}'
  jq -e '.isError != true' <<<"$output" >/dev/null || fail "tool failed: $output"
  [[ ! -e $HOME/project/download.html ]] || fail "download.html landed in the caller's cwd"
  [[ -z $(ls -A "$HOME/project") ]] || fail "the caller's cwd was written to: $(ls -A "$HOME/project")"
}

function without_json_the_call_is_unchanged { # @test
  call_api '{"command": "drive files list", "params": "{\"pageSize\": 1}"}'
  jq -e '.isError != true' <<<"$output" >/dev/null || fail "tool failed: $output"
  local expected
  expected=$(printf 'drive\tfiles\tlist\t--params\t%s\t' '{"pageSize": 1}')
  [[ $(cat "$GWS_STUB_LOG") == "$expected" ]] || fail "argv: $(cat "$GWS_STUB_LOG")"
}
