#! /usr/bin/env bats

setup() {
  load "$BATS_TEST_DIRNAME/common.bash"
  setup_test_home
  export output
}

teardown() {
  teardown_test_home
}

function no_config_permits_all { # @test
  local testfile="$HOME/test.txt"
  printf "content" >"$testfile"

  run_folio_mcp resources/read "{\"uri\":\"folio://read/${testfile}\"}"
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q "content"
}

function allow_rule_permits_match { # @test
  mkdir -p "$HOME/.config/folio"
  cat >"$HOME/.config/folio/folio.toml" <<EOF
[[permissions.allow]]
path = ["$HOME"]
EOF

  local testfile="$HOME/test.txt"
  printf "allowed" >"$testfile"

  run_folio_mcp resources/read "{\"uri\":\"folio://read/${testfile}\"}"
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q "allowed"
}

function deny_rule_blocks_match { # @test
  mkdir -p "$HOME/.config/folio"
  cat >"$HOME/.config/folio/folio.toml" <<EOF
[[permissions.deny]]
path = ["$HOME/secret"]
EOF

  mkdir -p "$HOME/secret"
  printf "secret data" >"$HOME/secret/file.txt"

  run_folio_mcp resources/read "{\"uri\":\"folio://read/${HOME}/secret/file.txt\"}"
  assert_success
  # JSON-RPC error means .result is null.
  [[ $output == "null" ]]
}

function deny_wins_over_allow { # @test
  mkdir -p "$HOME/.config/folio"
  cat >"$HOME/.config/folio/folio.toml" <<EOF
[[permissions.allow]]
path = ["$HOME"]

[[permissions.deny]]
path = ["$HOME/private"]
EOF

  mkdir -p "$HOME/private"
  printf "private data" >"$HOME/private/file.txt"

  # Allowed path works.
  printf "public data" >"$HOME/public.txt"
  run_folio_mcp resources/read "{\"uri\":\"folio://read/${HOME}/public.txt\"}"
  assert_success
  local text
  text=$(echo "$output" | jq -r '.contents[0].text')
  echo "$text" | grep -q "public"

  # Denied path blocked.
  run_folio_mcp resources/read "{\"uri\":\"folio://read/${HOME}/private/file.txt\"}"
  assert_success
  [[ $output == "null" ]]
}
