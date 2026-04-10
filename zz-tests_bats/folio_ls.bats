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
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cp "$BATS_TEST_DIRNAME/../builtin-servers/folio.toml" "$builtin_dir/"

  # Create a directory with known contents
  local test_dir="$HOME/project/testdir"
  mkdir -p "$test_dir/subdir"
  echo "hello" > "$test_dir/file1.txt"
  echo "world" > "$test_dir/file2.txt"
  ln -s "$test_dir/file1.txt" "$test_dir/link1"

  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  local params
  params=$(jq -cn --arg n "folio.ls" --arg p "$test_dir" \
    '{name: $n, arguments: {path: $p}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  # Should list all entries
  assert_output --partial "file1.txt"
  assert_output --partial "file2.txt"
  assert_output --partial "subdir"
  assert_output --partial "link1"
}

function folio_ls_shows_entry_types { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cp "$BATS_TEST_DIRNAME/../builtin-servers/folio.toml" "$builtin_dir/"

  local test_dir="$HOME/project/testdir"
  mkdir -p "$test_dir/subdir"
  echo "hello" > "$test_dir/file.txt"
  ln -s "$test_dir/file.txt" "$test_dir/link"

  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  local params
  params=$(jq -cn --arg n "folio.ls" --arg p "$test_dir" \
    '{name: $n, arguments: {path: $p}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  # Extract the text content and parse as JSON
  local entries
  entries=$(echo "$output" | jq -r '.content[0].text')

  # Check types
  echo "$entries" | jq -e '.[] | select(.name == "file.txt" and .type == "file")'
  echo "$entries" | jq -e '.[] | select(.name == "subdir" and .type == "directory")'
  echo "$entries" | jq -e '.[] | select(.name == "link" and .type == "symlink")'
}

function folio_ls_defaults_to_cwd { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cp "$BATS_TEST_DIRNAME/../builtin-servers/folio.toml" "$builtin_dir/"

  mkdir -p "$HOME/project"
  echo "hello" > "$HOME/project/readme.txt"

  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  local params
  params=$(jq -cn --arg n "folio.ls" '{name: $n, arguments: {}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "readme.txt"
}
