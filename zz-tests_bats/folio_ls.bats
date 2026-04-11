#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function folio_ls_lists_directory_contents { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp -r "$BATS_TEST_DIRNAME/../moxins/folio" "$moxin_dir/"

  # Create project dir with known contents (ls within CWD)
  mkdir -p "$HOME/project/testdir/subdir"
  echo "hello" > "$HOME/project/testdir/file1.txt"
  echo "world" > "$HOME/project/testdir/file2.txt"
  ln -s "$HOME/project/testdir/file1.txt" "$HOME/project/testdir/link1"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  local params
  params=$(jq -cn --arg n "folio.ls" \
    '{name: $n, arguments: {path: "testdir"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  local text
  text=$(echo "$output" | jq -r '.content[0].resource.text')
  echo "$text" | grep -q "file1.txt"
  echo "$text" | grep -q "file2.txt"
  echo "$text" | grep -q "subdir"
  echo "$text" | grep -q "link1"
}

function folio_ls_shows_entry_types { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp -r "$BATS_TEST_DIRNAME/../moxins/folio" "$moxin_dir/"

  mkdir -p "$HOME/project/testdir/subdir"
  echo "hello" > "$HOME/project/testdir/file.txt"
  ln -s "$HOME/project/testdir/file.txt" "$HOME/project/testdir/link"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  local params
  params=$(jq -cn --arg n "folio.ls" \
    '{name: $n, arguments: {path: "testdir"}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success

  # Verify resource block with mimeType and cache URI
  echo "$output" | jq -e '.content[0].type == "resource"'
  echo "$output" | jq -e '.content[0].resource.mimeType == "application/json"'
  echo "$output" | jq -e '.content[0].resource.uri | startswith("moxy.native://results/")'

  local entries
  entries=$(echo "$output" | jq -r '.content[0].resource.text')

  echo "$entries" | jq -e '.[] | select(.name == "file.txt" and .type == "file")'
  echo "$entries" | jq -e '.[] | select(.name == "subdir" and .type == "directory")'
  echo "$entries" | jq -e '.[] | select(.name == "link" and .type == "symlink")'
}

function folio_ls_defaults_to_cwd { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp -r "$BATS_TEST_DIRNAME/../moxins/folio" "$moxin_dir/"

  mkdir -p "$HOME/project"
  echo "hello" > "$HOME/project/readme.txt"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  local params
  params=$(jq -cn --arg n "folio.ls" '{name: $n, arguments: {}}')
  run_moxy_mcp_v1 "tools/call" "$params"
  assert_success
  echo "$output" | jq -r '.content[0].resource.text' | grep -q "readme.txt"
}
