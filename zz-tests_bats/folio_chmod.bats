#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function folio_chmod_sets_executable_bit { # @test
  mkdir -p "$HOME/project"
  echo "echo hi" > "$HOME/project/script.sh"
  chmod 644 "$HOME/project/script.sh"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.chmod" \
    '{name: $n, arguments: {path: "script.sh", mode: "+x"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  [ -x "$HOME/project/script.sh" ]
}

function folio_chmod_sets_octal_mode { # @test
  mkdir -p "$HOME/project"
  echo "data" > "$HOME/project/file.txt"
  chmod 777 "$HOME/project/file.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.chmod" \
    '{name: $n, arguments: {path: "file.txt", mode: "0644"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  local mode
  mode=$(stat -c '%a' "$HOME/project/file.txt")
  [ "$mode" = "644" ]
}

function folio_chmod_rejects_path_outside_cwd { # @test
  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"
  echo "data" > "$HOME/other/file.txt"
  chmod 600 "$HOME/other/file.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.chmod" --arg p "$HOME/other/file.txt" \
    '{name: $n, arguments: {path: $p, mode: "777"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "outside CWD"

  local mode
  mode=$(stat -c '%a' "$HOME/other/file.txt")
  [ "$mode" = "600" ]
}

function folio_chmod_rejects_missing_path { # @test
  mkdir -p "$HOME/project"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio.chmod" \
    '{name: $n, arguments: {path: "ghost", mode: "+x"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success
  assert_output --partial "no such file"
}

function folio_external_chmod_works_outside_cwd { # @test
  mkdir -p "$HOME/project"
  mkdir -p "$HOME/other"
  echo "data" > "$HOME/other/file.txt"
  chmod 644 "$HOME/other/file.txt"

  cd "$HOME/project"

  local params
  params=$(jq -cn --arg n "folio-external.chmod" --arg p "$HOME/other/file.txt" \
    '{name: $n, arguments: {path: $p, mode: "+x"}}')
  run_moxy_mcp "tools/call" "$params"
  assert_success

  [ -x "$HOME/other/file.txt" ]
}
