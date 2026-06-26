#! /usr/bin/env bats

# bats file_tags=folio
#
# Coverage for the #327 consolidations: folio.read absorbed read-range
# (start/end) and read-excluding (delete_start/delete_end); folio.tar
# absorbed tar-list (no member) and tar-cat (member).

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
}

teardown() {
  teardown_test_home
}

make_lines() {
  mkdir -p "$HOME/project"
  printf 'alpha\nbravo\ncharlie\ndelta\necho\n' >"$HOME/project/lines.txt"
  cd "$HOME/project"
}

function folio_read_range_prints_inclusive_range { # @test
  make_lines
  local params
  params=$(jq -cn --arg n "folio.read" \
    '{name: $n, arguments: {file_path: "lines.txt", start: 2, end: 3}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  echo "$text" | grep -q "bravo"
  echo "$text" | grep -q "charlie"
  ! echo "$text" | grep -q "alpha"
  ! echo "$text" | grep -q "delta"
}

function folio_read_excluding_omits_inclusive_range { # @test
  make_lines
  local params
  params=$(jq -cn --arg n "folio.read" \
    '{name: $n, arguments: {file_path: "lines.txt", delete_start: 2, delete_end: 4}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  echo "$text" | grep -q "alpha"
  echo "$text" | grep -q "echo"
  ! echo "$text" | grep -q "bravo"
  ! echo "$text" | grep -q "delta"
}

function folio_read_rejects_both_range_pairs { # @test
  make_lines
  local params
  params=$(jq -cn --arg n "folio.read" \
    '{name: $n, arguments: {file_path: "lines.txt", start: 1, end: 2, delete_start: 3, delete_end: 4}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  assert_output --partial "mutually exclusive"
}

make_tarball() {
  mkdir -p "$HOME/project/src/nested"
  echo "file one" >"$HOME/project/src/one.txt"
  echo "file two" >"$HOME/project/src/nested/two.txt"
  cd "$HOME/project"
  tar -czf bundle.tar.gz src
}

function folio_tar_lists_entries { # @test
  make_tarball
  local params
  params=$(jq -cn --arg n "folio.tar" \
    '{name: $n, arguments: {path: "bundle.tar.gz"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  assert_output --partial "src/one.txt"
  assert_output --partial "src/nested/two.txt"
}

function folio_tar_lists_with_pattern { # @test
  make_tarball
  local params
  params=$(jq -cn --arg n "folio.tar" \
    '{name: $n, arguments: {path: "bundle.tar.gz", pattern: "nested/"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  assert_output --partial "src/nested/two.txt"
  refute_output --partial "src/one.txt"
}

function folio_tar_cats_member { # @test
  make_tarball
  local params
  params=$(jq -cn --arg n "folio.tar" \
    '{name: $n, arguments: {path: "bundle.tar.gz", member: "src/one.txt"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  assert_output --partial "file one"
}

function folio_tar_rejects_pattern_with_member { # @test
  make_tarball
  local params
  params=$(jq -cn --arg n "folio.tar" \
    '{name: $n, arguments: {path: "bundle.tar.gz", member: "src/one.txt", pattern: "x"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  assert_output --partial "pattern only applies when listing"
}
