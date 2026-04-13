#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function folio_read_allows_file_within_cwd { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp -r "$BATS_TEST_DIRNAME/../build/moxins/folio" "$moxin_dir/"

  mkdir -p "$HOME/project"
  echo "hello world" > "$HOME/project/test.txt"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  local params
  params=$(jq -cn --arg n "folio.read" \
    '{name: $n, arguments: {file_path: "test.txt"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "hello world"
}

function folio_read_rejects_absolute_path_outside_cwd { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp -r "$BATS_TEST_DIRNAME/../build/moxins/folio" "$moxin_dir/"

  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"
  echo "secret" > "$HOME/other/secret.txt"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  local params
  params=$(jq -cn --arg n "folio.read" --arg p "$HOME/other/secret.txt" \
    '{name: $n, arguments: {file_path: $p}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "outside CWD"
  assert_output --partial "folio-external"
}

function folio_read_rejects_dotdot_traversal { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp -r "$BATS_TEST_DIRNAME/../build/moxins/folio" "$moxin_dir/"

  mkdir -p "$HOME/project"
  echo "secret" > "$HOME/secret.txt"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  local params
  params=$(jq -cn --arg n "folio.read" \
    '{name: $n, arguments: {file_path: "../secret.txt"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "outside CWD"
}

function folio_ls_rejects_path_outside_cwd { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp -r "$BATS_TEST_DIRNAME/../build/moxins/folio" "$moxin_dir/"

  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  local params
  params=$(jq -cn --arg n "folio.ls" --arg p "$HOME/other" \
    '{name: $n, arguments: {path: $p}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "outside CWD"
}

function folio_write_rejects_path_outside_cwd { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp -r "$BATS_TEST_DIRNAME/../build/moxins/folio" "$moxin_dir/"

  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  local params
  params=$(jq -cn --arg n "folio.write" --arg p "$HOME/other/evil.txt" \
    '{name: $n, arguments: {file_path: $p, content: "pwned"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "outside CWD"
  # Verify file was NOT created
  [ ! -f "$HOME/other/evil.txt" ]
}

function folio_read_allows_dev_fd_path { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp -r "$BATS_TEST_DIRNAME/../build/moxins/folio" "$moxin_dir/"

  mkdir -p "$HOME/project"
  echo "fd content" > "$HOME/project/test.txt"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  # Pass a /dev/fd path by using process substitution via a temp file
  # that moxy's native server will open as an fd. We simulate this by
  # reading a file whose content is delivered via /dev/fd — the key
  # assertion is that assert_within_cwd does not reject /dev/fd/N paths
  # on Linux where realpath resolves /dev/fd to /proc/self/fd.
  local params
  params=$(jq -cn --arg n "folio.read" \
    '{name: $n, arguments: {file_path: "/dev/fd/0"}}')
  # Feed content via stdin so /dev/fd/0 is readable
  run_moxy_mcp "tools/call" "$params" <<< "stdin line"
  assert_success
  # Should NOT contain the "outside CWD" error
  refute_output --partial "outside CWD"
}

function folio_external_allows_path_outside_cwd { # @test
  local moxin_dir="$BATS_TEST_TMPDIR/moxins"
  mkdir -p "$moxin_dir"
  cp -r "$BATS_TEST_DIRNAME/../build/moxins/folio-external" "$moxin_dir/"

  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"
  echo "accessible" > "$HOME/other/file.txt"

  cd "$HOME/project"
  export MOXIN_PATH="$moxin_dir"

  local params
  params=$(jq -cn --arg n "folio-external.read" --arg p "$HOME/other/file.txt" \
    '{name: $n, arguments: {file_path: $p}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "accessible"
}
