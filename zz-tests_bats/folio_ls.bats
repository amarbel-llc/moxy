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

  # Create project dir with known contents (ls within CWD)
  mkdir -p "$HOME/project/testdir/subdir"
  echo "hello" > "$HOME/project/testdir/file1.txt"
  echo "world" > "$HOME/project/testdir/file2.txt"
  ln -s "$HOME/project/testdir/file1.txt" "$HOME/project/testdir/link1"

  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  local params
  params=$(jq -cn --arg n "folio.ls" \
    '{name: $n, arguments: {path: "testdir"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  assert_output --partial "file1.txt"
  assert_output --partial "file2.txt"
  assert_output --partial "subdir"
  assert_output --partial "link1"
}

function folio_ls_shows_entry_types { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cp "$BATS_TEST_DIRNAME/../builtin-servers/folio.toml" "$builtin_dir/"

  mkdir -p "$HOME/project/testdir/subdir"
  echo "hello" > "$HOME/project/testdir/file.txt"
  ln -s "$HOME/project/testdir/file.txt" "$HOME/project/testdir/link"

  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  local params
  params=$(jq -cn --arg n "folio.ls" \
    '{name: $n, arguments: {path: "testdir"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  local entries
  entries=$(echo "$output" | jq -r '.content[0].text')

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
