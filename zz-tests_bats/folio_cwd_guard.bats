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
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cp "$BATS_TEST_DIRNAME/../builtin-servers/folio.toml" "$builtin_dir/"

  mkdir -p "$HOME/project"
  echo "hello world" > "$HOME/project/test.txt"

  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  local params
  params=$(jq -cn --arg n "folio.read" \
    '{name: $n, arguments: {file_path: "test.txt"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "hello world"
}

function folio_read_rejects_absolute_path_outside_cwd { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cp "$BATS_TEST_DIRNAME/../builtin-servers/folio.toml" "$builtin_dir/"

  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"
  echo "secret" > "$HOME/other/secret.txt"

  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  local params
  params=$(jq -cn --arg n "folio.read" --arg p "$HOME/other/secret.txt" \
    '{name: $n, arguments: {file_path: $p}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "outside CWD"
  assert_output --partial "folio-external"
}

function folio_read_rejects_dotdot_traversal { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cp "$BATS_TEST_DIRNAME/../builtin-servers/folio.toml" "$builtin_dir/"

  mkdir -p "$HOME/project"
  echo "secret" > "$HOME/secret.txt"

  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  local params
  params=$(jq -cn --arg n "folio.read" \
    '{name: $n, arguments: {file_path: "../secret.txt"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "outside CWD"
}

function folio_ls_rejects_path_outside_cwd { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cp "$BATS_TEST_DIRNAME/../builtin-servers/folio.toml" "$builtin_dir/"

  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"

  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  local params
  params=$(jq -cn --arg n "folio.ls" --arg p "$HOME/other" \
    '{name: $n, arguments: {path: $p}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "outside CWD"
}

function folio_write_rejects_path_outside_cwd { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cp "$BATS_TEST_DIRNAME/../builtin-servers/folio.toml" "$builtin_dir/"

  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"

  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  local params
  params=$(jq -cn --arg n "folio.write" --arg p "$HOME/other/evil.txt" \
    '{name: $n, arguments: {file_path: $p, content: "pwned"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "outside CWD"
  # Verify file was NOT created
  [ ! -f "$HOME/other/evil.txt" ]
}

function folio_external_allows_path_outside_cwd { # @test
  local builtin_dir="$BATS_TEST_TMPDIR/builtin-servers"
  mkdir -p "$builtin_dir"
  cp "$BATS_TEST_DIRNAME/../builtin-servers/folio-external.toml" "$builtin_dir/"

  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"
  echo "accessible" > "$HOME/other/file.txt"

  cd "$HOME/project"
  export MOXY_BUILTIN_DIR="$builtin_dir"

  local params
  params=$(jq -cn --arg n "folio-external.read" --arg p "$HOME/other/file.txt" \
    '{name: $n, arguments: {file_path: $p}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "accessible"
}
