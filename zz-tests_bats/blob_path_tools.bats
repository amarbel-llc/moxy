#! /usr/bin/env bats

# bats file_tags=folio

# moxy#469: rg.search, folio.file-type and folio.tar take a cached result
# (madder://blobs/<digest>) as their path. moxy hands the tool a /dev/fd/N
# pipe, which each of them has to read as content rather than as a path.

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  mkdir -p "$HOME/project"
  cd "$HOME/project"
}

teardown() {
  teardown_test_home
}

# seed_blob FILE: writes FILE into the per-test .default store, prints its id.
seed_blob() {
  _ensure_madder_default_store
  (cd "$HOME" && "${MADDER_BIN:-madder}" write -format json .default "$1") | jq -r 'select(.id) | .id'
}

result_text() { jq -r '.content[0] | (.text // .resource.text // "")' <<<"$output"; }

call() { # NAME ARGS_JSON
  run_moxy_mcp_v1 "tools/call" "$(jq -cn --arg n "$1" --argjson a "$2" '{name: $n, arguments: $a}')"
  assert_success
  jq -e '.isError != true' <<<"$output" >/dev/null || fail "tool failed: $output"
}

function rg_search_finds_a_pattern_in_a_blob { # @test
  printf 'alpha\nneedle here\nomega\n' >"$BATS_TEST_TMPDIR/blob"
  local id
  id=$(seed_blob "$BATS_TEST_TMPDIR/blob")

  call rg.search "$(jq -cn --arg p "madder://blobs/$id" '{pattern: "needle", path: $p, output_mode: "content"}')"
  [[ $(result_text) == *"needle here"* ]] || fail "$output"
}

function rg_search_a_blob_without_the_pattern_matches_nothing { # @test
  printf 'alpha\nomega\n' >"$BATS_TEST_TMPDIR/blob"
  local id
  id=$(seed_blob "$BATS_TEST_TMPDIR/blob")

  call rg.search "$(jq -cn --arg p "madder://blobs/$id" '{pattern: "needle", path: $p, output_mode: "content"}')"
  [[ $(result_text) != *needle* ]] || fail "$output"
}

function file_type_reports_a_blobs_content { # @test
  printf '#!/bin/sh\necho hi\n' >"$BATS_TEST_TMPDIR/blob"
  local id
  id=$(seed_blob "$BATS_TEST_TMPDIR/blob")

  call folio.file-type "$(jq -cn --arg p "madder://blobs/$id" '{path: $p}')"
  [[ $(result_text) == *script* ]] || fail "$output"
  [[ $(result_text) != *pipe* ]] || fail "$output"
}

function tar_lists_and_extracts_from_a_blob { # @test
  mkdir pkg
  echo "member body" >pkg/inside.txt
  tar -czf "$BATS_TEST_TMPDIR/blob" pkg
  local id
  id=$(seed_blob "$BATS_TEST_TMPDIR/blob")

  call folio.tar "$(jq -cn --arg p "madder://blobs/$id" '{path: $p}')"
  [[ $(result_text) == *pkg/inside.txt* ]] || fail "$output"

  call folio.tar "$(jq -cn --arg p "madder://blobs/$id" '{path: $p, member: "pkg/inside.txt"}')"
  [[ $(result_text) == *"member body"* ]] || fail "$output"
}
