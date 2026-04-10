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
  cp "$BATS_TEST_DIRNAME/../moxins/folio.toml" "$moxin_dir/"

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
  run_moxy_mcp "tools/call" "$params"
  assert_success

  assert_output --partial "file1.txt"
  assert_output --partial "file2.txt"
  assert_output --partial "subdir"
  assert_output --partial "link1"
}

function folio_ls_shows_entry_types { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp "$BATS_TEST_DIRNAME/../moxins/folio.toml" "$moxin_dir/"

  mkdir -p "$HOME/project/testdir/subdir"
  echo "hello" > "$HOME/project/testdir/file.txt"
  ln -s "$HOME/project/testdir/file.txt" "$HOME/project/testdir/link"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

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
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp "$BATS_TEST_DIRNAME/../moxins/folio.toml" "$moxin_dir/"

  mkdir -p "$HOME/project"
  echo "hello" > "$HOME/project/readme.txt"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  local params
  params=$(jq -cn --arg n "folio.ls" '{name: $n, arguments: {}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "readme.txt"
}
