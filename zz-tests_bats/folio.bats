#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function resource_templates_listed { # @test
  run_folio_mcp resources/templates/list
  assert_success
  echo "$output" | jq -e '.resourceTemplates[] | select(.uriTemplate | contains("folio://read/"))'
}

function tools_listed { # @test
  run_folio_mcp tools/list
  assert_success
  echo "$output" | jq -e '.tools[] | select(.name == "write")'
  echo "$output" | jq -e '.tools[] | select(.name == "edit")'
}

function read_returns_content_with_line_numbers { # @test
  local testfile="$HOME/test.txt"
  printf "line one\nline two\nline three\n" >"$testfile"

  run_folio_mcp resources/read "{\"uri\":\"folio://read/${testfile}\"}"
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q "1	line one"
  echo "$text" | grep -q "3	line three"
}

function read_with_offset_and_limit { # @test
  local testfile="$HOME/test.txt"
  printf "a\nb\nc\nd\ne\n" >"$testfile"

  run_folio_mcp resources/read "{\"uri\":\"folio://read/${testfile}?offset=2&limit=2\"}"
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q "2	b"
  echo "$text" | grep -q "3	c"
  # Line 4 should not appear.
  ! echo "$text" | grep -q "4	d"
}

function read_nonexistent_file_returns_error { # @test
  run_folio_mcp resources/read '{"uri":"folio://read//nonexistent/path/file.txt"}'
  assert_success
  # JSON-RPC error means .result is null (the error is at the top-level .error field).
  [[ $output == "null" ]]
}

function read_binary_file_returns_error { # @test
  local binfile="$HOME/binary.bin"
  printf '\x89PNG\x00\x00' >"$binfile"

  run_folio_mcp resources/read "{\"uri\":\"folio://read/${binfile}\"}"
  assert_success
  # JSON-RPC error means .result is null.
  [[ $output == "null" ]]
}

function write_creates_new_file { # @test
  local testfile="$HOME/newfile.txt"

  run_folio_mcp tools/call "$(jq -cn --arg p "$testfile" '{
    "name": "write",
    "arguments": {"file_path": $p, "content": "hello world"}
  }')"
  assert_success
  echo "$output" | jq -e '.content[0].text | contains("Wrote")'

  [[ -f $testfile ]]
  [[ "$(cat "$testfile")" == "hello world" ]]
}

function write_creates_parent_directories { # @test
  local testfile="$HOME/deep/nested/dir/file.txt"

  run_folio_mcp tools/call "$(jq -cn --arg p "$testfile" '{
    "name": "write",
    "arguments": {"file_path": $p, "content": "nested content"}
  }')"
  assert_success
  [[ -f $testfile ]]
  [[ "$(cat "$testfile")" == "nested content" ]]
}

function edit_replaces_unique_match { # @test
  local testfile="$HOME/edit.txt"
  printf "hello world" >"$testfile"

  run_folio_mcp tools/call "$(jq -cn --arg p "$testfile" '{
    "name": "edit",
    "arguments": {"file_path": $p, "old_string": "world", "new_string": "universe"}
  }')"
  assert_success
  echo "$output" | jq -e '.content[0].text | contains("1 replacement")'
  [[ "$(cat "$testfile")" == "hello universe" ]]
}

function edit_rejects_zero_matches { # @test
  local testfile="$HOME/edit.txt"
  printf "hello world" >"$testfile"

  run_folio_mcp tools/call "$(jq -cn --arg p "$testfile" '{
    "name": "edit",
    "arguments": {"file_path": $p, "old_string": "missing", "new_string": "replacement"}
  }')"
  assert_success
  echo "$output" | jq -e '.isError == true'
}

function edit_rejects_ambiguous_match { # @test
  local testfile="$HOME/edit.txt"
  printf "foo bar foo baz" >"$testfile"

  run_folio_mcp tools/call "$(jq -cn --arg p "$testfile" '{
    "name": "edit",
    "arguments": {"file_path": $p, "old_string": "foo", "new_string": "qux"}
  }')"
  assert_success
  echo "$output" | jq -e '.isError == true'
}

function edit_replace_all { # @test
  local testfile="$HOME/edit.txt"
  printf "foo bar foo baz" >"$testfile"

  run_folio_mcp tools/call "$(jq -cn --arg p "$testfile" '{
    "name": "edit",
    "arguments": {"file_path": $p, "old_string": "foo", "new_string": "qux", "replace_all": true}
  }')"
  assert_success
  echo "$output" | jq -e '.content[0].text | contains("2 replacement")'
  [[ "$(cat "$testfile")" == "qux bar qux baz" ]]
}
