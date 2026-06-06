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

function folio_ls_lists_directory_contents { # @test
  # Create project dir with known contents (ls within CWD)
  mkdir -p "$HOME/project/testdir/subdir"
  echo "hello" > "$HOME/project/testdir/file1.txt"
  echo "world" > "$HOME/project/testdir/file2.txt"
  ln -s "$HOME/project/testdir/file1.txt" "$HOME/project/testdir/link1"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.ls" \
    '{name: $n, arguments: {path: "testdir"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  # Small output under the default cache-results = "threshold" policy
  # (#319): plain text block, no resource wrapper.
  local text
  text=$(echo "$output" | jq -r '.content[0].text')
  echo "$text" | grep -q "file1.txt"
  echo "$text" | grep -q "file2.txt"
  echo "$text" | grep -q "subdir"
  echo "$text" | grep -q "link1"
}

function folio_ls_shows_entry_types { # @test
  mkdir -p "$HOME/project/testdir/subdir"
  echo "hello" > "$HOME/project/testdir/file.txt"
  ln -s "$HOME/project/testdir/file.txt" "$HOME/project/testdir/link"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.ls" \
    '{name: $n, arguments: {path: "testdir"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  # Small ls output under the default cache-results = "threshold" policy
  # (#319): plain text block, mime dropped, no blob write. The script still
  # emits a mime-bearing envelope block; moxy strips it below threshold.
  echo "$output" | jq -e '.content[0].type == "text"' || fail '.content[0].type == "text" check failed: '"$output"
  echo "$output" | jq -e '.content[0] | has("mimeType") | not' || fail 'mimeType should be dropped: '"$output"

  local entries
  entries=$(echo "$output" | jq -r '.content[0].text')

  echo "$entries" | jq -e '.[] | select(.name == "file.txt" and .type == "file")' || fail '.[] | select(.name == "file.txt" and .type == "file") check failed: '"$entries"
  echo "$entries" | jq -e '.[] | select(.name == "subdir" and .type == "directory")' || fail '.[] | select(.name == "subdir" and .type == "directory") check failed: '"$entries"
  echo "$entries" | jq -e '.[] | select(.name == "link" and .type == "symlink")' || fail '.[] | select(.name == "link" and .type == "symlink") check failed: '"$entries"
}

function folio_ls_defaults_to_cwd { # @test
  mkdir -p "$HOME/project"
  echo "hello" > "$HOME/project/readme.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.ls" '{name: $n, arguments: {}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  echo "$output" | jq -r '.content[0].text' | grep -q "readme.txt"
}
